package credit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归“读取免修历史时，状态为 rejected 的申请必须保留含实际文字的
// 拒绝原因”。缺失包括：reason 字段没有出现、值为 JSON null、空字符串，以及
// 只含空白（普通空格、制表符、换行、全角空格 U+3000、不换行空格 U+00A0
// 混用）的字符串。这类不完整历史无法经正常申请流程落入文件（被拒绝时总会
// 记下原因），因此损坏用例直接构造记录文件，聚焦 Load 读取历史这一路径：
//   - 只要文件中有一份这样的已拒绝申请，就整份拒绝，退出码语义为内容损坏，
//     不返回可继续核对或保存的部分记录，原文件逐字节保留；
//     哪怕申请不属于本次查询的学生，或目标要求已有通过修读、有效免修、照常
//     能算出学分，也不能跳过这份历史；
//   - 字段名大小写与 Unicode 转义沿用既有识别规则（reason/Reason/REASON 及
//     解码后等同的转义写法都指向同一字段），字段在对象中的位置不影响结果；
//   - 有文字的原因按原文保留（含前后、中间空白），不要求固定措辞，也不按
//     当前课程要求重新判断旧申请；因依据为空被拒绝的申请仍允许保留空依据；
//   - 有效免修原本可以没有 reason，已撤销免修沿用既有读取与核对行为，二者
//     都不增加原因限制。

// JSON 源文本中 reason 键的 \uXXXX 转义片段（不含两侧引号）。Go 双引号
// 字符串里 "\\u" 即字面反斜线+u，写入记录文件后 JSON 解码分别得到
// "reason"、"REASON" 与大写 "R"，正好模拟记录文件中的 Unicode 转义写法。
const (
	escapedReasonKeyLower = "\\u0072eason"
	escapedReasonKeyUpper = "\\u0052EASON"
	escapedUpperR         = "\\u0052"
)

// rejectReasonBase 构造结构完整、引用齐全的基础记录：学生 s1 的要求 r1
// 指向 4 学分课程 c1。
func rejectReasonBase() *fileData {
	return &fileData{
		Version:      recordVersion,
		Courses:      []*Course{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
	}
}

// rejectWaiverRaw 直接拼出一份只含一条 rejected 免修的记录，原因部分由
// reasonPart 控制（如 `,"reason":null`、`,"reason":""` 或空串表示缺字段）。
func rejectWaiverRaw(waiverJSON string) string {
	return "{\n" +
		`  "version": 1,` + "\n" +
		`  "courses": [{"id": "c1", "name": "数学", "credit": 4, "open": true}],` + "\n" +
		`  "students": [{"id": "s1"}],` + "\n" +
		`  "requirements": [{"id": "r1", "student": "s1", "course": "c1"}],` + "\n" +
		`  "waivers": [` + waiverJSON + `],` + "\n" +
		`  "nextResultSeq": 0` + "\n" +
		"}\n"
}

// assertRejectReasonFileCorrupt 是损坏用例的共同断言：Load 失败、标记文件
// 已存在、错误点名问题文件/学生/免修编号并说明缺少拒绝原因，不返回可用
// store，且原文件字节完整保留。
func assertRejectReasonFileCorrupt(t *testing.T, path string, raw []byte, student, waiver string) {
	t.Helper()
	s, existed, err := Load(path)
	if err == nil {
		t.Fatalf("已拒绝免修缺少原因时必须整份拒绝，却得到 store=%v", s)
	}
	if s != nil {
		t.Fatalf("拒绝时不得返回可继续核对或保存的记录集，得到 %v", s)
	}
	if !existed {
		t.Fatalf("损坏文件应标记为已存在，existed=%v err=%v", existed, err)
	}
	msg := err.Error()
	for _, want := range []string{path, "内容损坏", student, waiver, "已拒绝免修", "拒绝原因"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误信息应包含 %q（文件/学生/免修编号/缺少拒绝原因），得到：%v",
				want, err)
		}
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(raw) {
		t.Fatalf("拒绝读取不得改写原文件\nwant=%q\n got=%q", raw, got)
	}
}

// TestLoadRejectedWaiverMissingReasonAllForms 原因缺失的每种形式（字段缺
// 失、null、空串、各种空白混用、非文字值）都必须让整份文件损坏。
func TestLoadRejectedWaiverMissingReasonAllForms(t *testing.T) {
	// 用结构体构造的各种“只有空白”的原因（编码后与直接书写不可区分）。
	blankReasons := map[string]string{
		"空字符串":         "",
		"普通空格":         "     ",
		"制表符换行回车":      "\t\n\r\n\t",
		"全角空格":         "　　　",
		"不换行空格":        "     ",
		"普通空白与全角不换行混用": " \t　 \n  \r　",
	}
	for name, reason := range blankReasons {
		t.Run("空白原因/"+name, func(t *testing.T) {
			d := rejectReasonBase()
			d.Waivers = []*Waiver{{
				ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "学科竞赛获奖",
				Status: WaiverRejected, Reason: reason,
			}}
			path, raw := writeRecord(t, d)
			assertRejectReasonFileCorrupt(t, path, raw, "s1", "w1")
		})
	}

	// 字段缺失、null、空串、数字、布尔、对象、数组等需要精确控制 JSON 写法
	// 的情形（结构体无法区分缺失、null 与空串，三者都编成空串）。
	rawCases := map[string]string{
		"字段缺失": `{"id":"w1","student":"s1","req":"r1","basis":"学科竞赛获奖",` +
			`"status":"rejected"}`,
		"reason为null": `{"id":"w1","student":"s1","req":"r1","basis":"学科竞赛获奖",` +
			`"status":"rejected","reason":null}`,
		"reason为空串": `{"id":"w1","student":"s1","req":"r1","basis":"学科竞赛获奖",` +
			`"status":"rejected","reason":""}`,
		"reason只有空格": `{"id":"w1","student":"s1","req":"r1","basis":"学科竞赛获奖",` +
			`"status":"rejected","reason":" \t\n 　"}`,
		"reason为数字": `{"id":"w1","student":"s1","req":"r1","basis":"学科竞赛获奖",` +
			`"status":"rejected","reason":0}`,
		"reason为布尔": `{"id":"w1","student":"s1","req":"r1","basis":"学科竞赛获奖",` +
			`"status":"rejected","reason":false}`,
		"reason为对象": `{"id":"w1","student":"s1","req":"r1","basis":"学科竞赛获奖",` +
			`"status":"rejected","reason":{}}`,
		"reason为数组": `{"id":"w1","student":"s1","req":"r1","basis":"学科竞赛获奖",` +
			`"status":"rejected","reason":["x"]}`,
		"status为null后缺原因不算本检查": "", // 占位，下面单独处理非法 status
	}
	delete(rawCases, "status为null后缺原因不算本检查")
	for name, waiverJSON := range rawCases {
		t.Run("原始JSON/"+name, func(t *testing.T) {
			content := rejectWaiverRaw(waiverJSON)
			path := filepath.Join(t.TempDir(), "records.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			assertRejectReasonFileCorrupt(t, path, []byte(content), "s1", "w1")
		})
	}
}

// TestLoadRejectedReasonKeyVariants reason/Reason/REASON 及解码后等同的
// Unicode 转义写法都指向同一原因字段：缺失或写成 null 时一律拒绝，字段
// 位置（在状态前后、编号之后）不影响结果。
func TestLoadRejectedReasonKeyVariants(t *testing.T) {
	cases := map[string]struct {
		key    string
		suffix string // 跟在键后的原因值写法
	}{
		"小写reason为null":   {"reason", `:null`},
		"大写Reason为null":   {"Reason", `:null`},
		"全大写REASON为null":  {"REASON", `:null`},
		"转义小写键为null":      {escapedReasonKeyLower, `:null`},
		"转义大写键为null":      {escapedReasonKeyUpper, `:null`},
		"部分转义混合大小写错为null": {"reaso" + escapedUpperR, `:null`},
		"小写reason为空串":     {"reason", `:""`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// 原因键放在最前，编号、学生都在其后：仍要在对象闭合时点对名。
			waiverJSON := `{"` + tc.key + `"` + tc.suffix +
				`,"id":"w1","student":"s1","req":"r1","basis":"依据","status":"rejected"}`
			content := rejectWaiverRaw(waiverJSON)
			path := filepath.Join(t.TempDir(), "records.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			assertRejectReasonFileCorrupt(t, path, []byte(content), "s1", "w1")
		})
	}

	// 同一条免修里只出现一次的 Reason 大写写法且含实际文字：照常识别读取。
	t.Run("大写Reason含文字照常读取", func(t *testing.T) {
		content := rejectWaiverRaw(
			`{"id":"w1","student":"s1","req":"r1","basis":"依据","status":"rejected","Reason":"当时材料不全"}`)
		path := filepath.Join(t.TempDir(), "records.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		s, existed, err := Load(path)
		if err != nil || !existed {
			t.Fatalf("只出现一次且含文字的大写 Reason 应正常读取，existed=%v err=%v", existed, err)
		}
		if w := s.Waiver("s1", "w1"); w == nil || w.Status != WaiverRejected ||
			w.Reason != "当时材料不全" {
			t.Fatalf("原因应按解码文字读入并保留，得到 %+v", w)
		}
	})
}

// TestLoadMissingRejectReasonNotMaskedByCredits 即使问题申请的目标要求已由
// 通过修读满足、另有有效免修，照常能算出总学分，也不能放过这份缺原因的
// 历史：不能拿着通过记录或有效免修输出核对结果。
func TestLoadMissingRejectReasonNotMaskedByCredits(t *testing.T) {
	t.Run("另有通过修读", func(t *testing.T) {
		d := rejectReasonBase()
		d.Enrollments = []*Enrollment{{
			ID: "e1", StudentID: "s1", ReqID: "r1", Term: "2024春",
			Result: Passed, ResultSeq: 1,
		}}
		d.NextResultSeq = 1
		// 缺原因的是另一条被拒绝历史（目标要求在当时不存在）。
		d.Waivers = []*Waiver{{
			ID: "wbad", StudentID: "s1", ReqID: "ghost", Basis: "旧材料",
			Status: WaiverRejected,
		}}
		path, raw := writeRecord(t, d)
		assertRejectReasonFileCorrupt(t, path, raw, "s1", "wbad")
	})

	t.Run("目标要求已有有效免修", func(t *testing.T) {
		d := rejectReasonBase()
		d.Waivers = []*Waiver{
			{ID: "wok", StudentID: "s1", ReqID: "r1", Basis: "有效依据", Status: WaiverApproved},
			{ID: "wbad", StudentID: "s1", ReqID: "r1", Basis: "再次申请",
				Status: WaiverRejected, Reason: "  \n　 "},
		}
		path, raw := writeRecord(t, d)
		assertRejectReasonFileCorrupt(t, path, raw, "s1", "wbad")
	})
}

// TestLoadMissingRejectReasonRejectsEntireFileForOtherStudent 文件里另有
// 完全合法的学生 s2，缺原因的被拒绝申请属于 s1（甚至不属于本次查询的学生）：
// 整份文件仍不可读，错误归属 s1，不能误报 s2，也不能只加载 s2 的部分记录。
func TestLoadMissingRejectReasonRejectsEntireFileForOtherStudent(t *testing.T) {
	d := rejectReasonBase()
	d.Courses = append(d.Courses, &Course{ID: "c2", Name: "线性代数", Credit: 3, Open: true})
	d.Students = append(d.Students, &Student{ID: "s2"})
	d.Requirements = append(d.Requirements, &Requirement{ID: "r1", StudentID: "s2", CourseID: "c2"})
	d.Waivers = []*Waiver{
		// s1 名下的缺原因历史：r1 现在已补建，原因文本也不再重新判定。
		{ID: "wbad", StudentID: "s1", ReqID: "r1", Basis: "旧材料",
			Status: WaiverRejected},
		// s2 本人合法、原因齐全的记录。
		{ID: "w1", StudentID: "s2", ReqID: "r1", Basis: "s2 的依据",
			Status: WaiverApproved},
	}
	path, raw := writeRecord(t, d)

	s, _, err := Load(path)
	if err == nil {
		t.Fatalf("属于 s1 的缺原因历史必须让整份文件不可读，得到 store=%v", s)
	}
	msg := err.Error()
	if !strings.Contains(msg, "s1") || !strings.Contains(msg, "wbad") {
		t.Fatalf("错误应归属 s1 的免修 wbad，得到：%v", err)
	}
	if strings.Contains(msg, "学生 s2") {
		t.Fatalf("s2 的记录合法，不应被点名为问题方，得到：%v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(raw) {
		t.Fatal("拒绝读取不得改写原文件")
	}
}

// TestLoadRejectedWaiverWithRealReasonPreserved 含实际文字的拒绝原因按原文
// 保留（包括前后与中间空白），不要求固定措辞，也不根据当前课程要求重新判断
// 旧申请；因依据为空被拒绝的申请仍允许保留空依据。这些历史继续出现在该
// 学生的拒绝列表中、不提供学分。
func TestLoadRejectedWaiverWithRealReasonPreserved(t *testing.T) {
	// 原因里保留前后与中间空白，重存后逐字不变。
	reason := "\t\n  当时目标要求尚未建立　 \n  拒绝成立  \n  "
	d := rejectReasonBase()
	d.Waivers = []*Waiver{
		// 当初因目标要求不存在被拒绝，后来 r1 已补建：保存着原原因的申请
		// 仍是合法的已拒绝历史，不重新判定、不复活。
		{ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "竞赛材料",
			Status: WaiverRejected, Reason: reason},
		// 因依据为空被拒绝：依据允许为空，只要原因是实际文字就合法。
		{ID: "w2", StudentID: "s1", ReqID: "ghost", Basis: "",
			Status: WaiverRejected, Reason: "免修依据为空"},
	}
	path, raw := writeRecord(t, d)

	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("含实际文字原因的拒绝历史应正常读取，existed=%v err=%v", existed, err)
	}
	if w := s.Waiver("s1", "w1"); w == nil || w.Status != WaiverRejected || w.Reason != reason {
		t.Fatalf("w1 原因应连前后/中间空白原样保留，得到 %+v", w)
	}
	if w := s.Waiver("s1", "w2"); w == nil || w.Basis != "" || w.Reason != "免修依据为空" {
		t.Fatalf("w2 应保留空依据与原因，得到 %+v", w)
	}

	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 0 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("被拒绝历史不提供学分，r1 应未满足，得到 学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	if len(rep.RejectedWaivers) != 2 {
		t.Fatalf("两条被拒绝历史都应列出，得到 %d 条", len(rep.RejectedWaivers))
	}
	for _, rj := range rep.RejectedWaivers {
		if strings.TrimSpace(rj.Reason) == "" {
			t.Fatalf("列出的拒绝原因必须是实际文字，得到 %+v", rj)
		}
	}

	// 重新保存应逐字节一致（原因未被修剪），再加载结论不变；只读/保存都
	// 不能改写原原因。
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	saved, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(saved) != string(raw) {
		t.Fatalf("重新保存不得修剪或改写原因\nwant=%q\n got=%q", raw, saved)
	}
	reloaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("重新保存后应可再次加载：%v", err)
	}
	if w := reloaded.Waiver("s1", "w1"); w == nil || w.Reason != reason {
		t.Fatalf("再次加载后原因原文应保持不变，得到 %+v", w)
	}
}

// TestLoadApprovedAndRevokedWaiverWithoutReasonUnchanged 有效免修原本可以
// 没有 reason，已撤销免修沿用既有读取与核对行为：两者都不增加原因限制，
// 本次保持可正常读取。
func TestLoadApprovedAndRevokedWaiverWithoutReasonUnchanged(t *testing.T) {
	t.Run("有效免修没有reason", func(t *testing.T) {
		d := rejectReasonBase()
		d.Waivers = []*Waiver{{
			ID: "wa", StudentID: "s1", ReqID: "r1", Basis: "竞赛获奖",
			Status: WaiverApproved, // 不设置 Reason
		}}
		path, _ := writeRecord(t, d)
		s, _, err := Load(path)
		if err != nil {
			t.Fatalf("有效免修原本可以没有原因，不应判坏：%v", err)
		}
		if rep := s.CheckStudent("s1"); rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
			t.Fatalf("有效免修应照常满足要求计 4 学分，得到 %+v", rep)
		}
	})

	t.Run("已撤销免修没有reason", func(t *testing.T) {
		d := rejectReasonBase()
		d.Waivers = []*Waiver{{
			ID: "wv", StudentID: "s1", ReqID: "r1", Basis: "原竞赛材料",
			Status: WaiverRevoked, // 撤销原因不增加限制
		}}
		path, _ := writeRecord(t, d)
		s, _, err := Load(path)
		if err != nil {
			t.Fatalf("已撤销免修沿用既有行为，不应因缺少原因判坏：%v", err)
		}
		rep := s.CheckStudent("s1")
		if rep.TotalCredits != 0 || len(rep.RevokedWaivers) != 1 || rep.RevokedWaivers[0] != "wv" {
			t.Fatalf("已撤销免修应照常列入撤销历史且不提供学分，得到 %+v", rep)
		}
	})
}

// TestLoadDataRejectsRejectedWaiverBlankReason 直接构造 fileData 调用
// loadData 也绕不过“拒绝历史必有原因”：缺失/空白原因（解码后无法与 null
// 区分的情形）在这一层同样返回损坏错误，使该不变量不依赖 token 扫描。
func TestLoadDataRejectsRejectedWaiverBlankReason(t *testing.T) {
	for name, reason := range map[string]string{
		"空原因":  "",
		"空白原因": " \t　\n ",
	} {
		t.Run(name, func(t *testing.T) {
			d := rejectReasonBase()
			d.Waivers = []*Waiver{{
				ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "依据",
				Status: WaiverRejected, Reason: reason,
			}}
			s := NewStore()
			err := s.loadData(d)
			if err == nil {
				t.Fatalf("loadData 应拒绝缺原因的已拒绝免修，却得到 store=%v", s)
			}
			msg := err.Error()
			if !strings.Contains(msg, "s1") || !strings.Contains(msg, "w1") ||
				!strings.Contains(msg, "拒绝原因") {
				t.Fatalf("错误应点名 s1 的免修 w1 缺少拒绝原因，得到：%v", err)
			}
		})
	}
}

// TestScanRejectedWaiverReasons 直接覆盖扫描器边界：只有状态值确为字符串
// "rejected" 且原因缺失/null/空白/非文字时才报错；approved、revoked、合法
// 原因、非免修对象中的 reason 键等都不应误报。
func TestScanRejectedWaiverReasons(t *testing.T) {
	badCases := map[string]string{
		"缺原因字段":          `{"students":[{"id":"s1"}],"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected"}]}`,
		"原因为null":        `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected","reason":null}]}`,
		"原因为空串":          `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected","reason":""}]}`,
		"原因只有空格":         `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected","reason":" \t\n 　"}]}`,
		"原因为数字":          `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected","reason":3}]}`,
		"原因为布尔":          `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected","reason":true}]}`,
		"原因为对象":          `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected","reason":{"text":"x"}}]}`,
		"原因为数组":          `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected","reason":["x"]}]}`,
		"原因键在最前缺值为null":  `{"waivers":[{"reason":null,"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected"}]}`,
		"大写Reason为null":  `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected","Reason":null}]}`,
		"全大写REASON为空串":   `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected","REASON":""}]}`,
		"转义小写原因为null":    `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected","` + escapedReasonKeyLower + `":null}]}`,
		"嵌套数组中的拒绝申请":     `{"waivers":[[{"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected"}]]}`,
		"顶层WAIVERS变体缺原因": `{"WAIVERS":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected"}]}`,
		"多条中第二条缺原因":      `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected","reason":"a"},{"id":"w2","student":"s1","req":"r","basis":"b","status":"rejected"}]}`,
	}
	for name, in := range badCases {
		t.Run("拒绝/"+name, func(t *testing.T) {
			var e *missingRejectReasonError
			err := scanRejectedWaiverReasons(strings.NewReader(in))
			if !errors.As(err, &e) {
				t.Fatalf("应返回 *missingRejectReasonError（输入 %q），得到 %T: %v", in, err, err)
			}
			if e.studentID != "s1" {
				t.Fatalf("应定位到学生 s1（编号在原因之后也行），得到 %q", e.studentID)
			}
		})
	}

	okCases := map[string]string{
		"拒绝申请有实际原因":            `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected","reason":"依据不足"}]}`,
		"原因只有前后空白但含文字":         `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected","reason":"\t 原因 \n"}]}`,
		"原因是文字null":            `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected","reason":"null"}]}`,
		"有效免修没有reason":         `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"approved"}]}`,
		"有效免修reason为null不属本检查": `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"approved","reason":null}]}`,
		"已撤销免修没有reason":        `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"revoked"}]}`,
		"已撤销免修原因为空白":           `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"revoked","reason":"  "}]}`,
		"status为null不判定拒绝原因":   `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":null}]}`,
		"status为数字不判定拒绝原因":     `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":2}]}`,
		"状态值大小写Rejected不算":     `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"Rejected"}]}`,
		"课程对象里的reason键不是免修":    `{"courses":[{"id":"c1","reason":"x"}]}`,
		"空免修列表":                `{"waivers":[]}`,
		"免修列表为null":            `{"waivers":null}`,
		"空输入留给解码阶段":            ``,
		"语法不完整留给解码阶段":          `{"waivers":[{"id":"w1","status":"rejected",`,
	}
	for name, in := range okCases {
		t.Run("放过/"+name, func(t *testing.T) {
			if err := scanRejectedWaiverReasons(strings.NewReader(in)); err != nil {
				t.Fatalf("不应报告缺少拒绝原因（输入 %q），得到 %v", in, err)
			}
		})
	}

	// 问题分类要准确：缺字段、null、空白、非文字各归各类。
	t.Run("问题分类", func(t *testing.T) {
		classify := func(in string) rejectReasonProblem {
			err := scanRejectedWaiverReasons(strings.NewReader(in))
			var e *missingRejectReasonError
			if !errors.As(err, &e) {
				t.Fatalf("应报错（输入 %q）：%v", in, err)
			}
			return e.problem
		}
		base := func(extra string) string {
			return `{"waivers":[{"id":"w1","student":"s1","req":"r","basis":"b","status":"rejected"` + extra + `}]}`
		}
		if p := classify(base(``)); p != reasonAbsent {
			t.Fatalf("缺字段应为 reasonAbsent，得到 %v", p)
		}
		if p := classify(base(`,"reason":null`)); p != reasonNull {
			t.Fatalf("null 应为 reasonNull，得到 %v", p)
		}
		if p := classify(base(`,"reason":" \t　"`)); p != reasonBlank {
			t.Fatalf("空白应为 reasonBlank，得到 %v", p)
		}
		if p := classify(base(`,"reason":1`)); p != reasonWrongType {
			t.Fatalf("数字应为 reasonWrongType，得到 %v", p)
		}
		if p := classify(base(`,"reason":{}`)); p != reasonWrongType {
			t.Fatalf("对象应为 reasonWrongType，得到 %v", p)
		}
	})
}
