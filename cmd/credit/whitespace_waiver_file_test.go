package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“waiver 按用户给出的完整学生编号、要求编号和免修
// 编号处理申请”。记录文件可以合法保存带前后空白（普通空格、制表符、全角
// 空格）的编号，查询、成绩提交与免修撤销都按完整编号匹配，申请免修也必须
// 如此：
//   - 给 " s1 " 提交申请只命中本人的要求并获得本人课程的学分，提示、
//     show 历史与 check 来源都保留实际使用的编号，"s1" 的学分、要求与
//     申请保持原样；
//   - 同一学生名下 "r1" 与 " r1 " 是两项要求、"w1" 与 " w1 " 是两份
//     申请，必须分别处理，不能因去空白返回另一份申请或报内容冲突；
//   - 完整学生编号不存在时按业务拒绝（退出码 1）说明学生不存在，不给
//     其他学生新增历史，记录文件逐字节保持原样；
//   - 学生存在而完整要求编号不存在时，即使去掉空白能找到要求也拒绝该
//     申请（退出码 1），但按原规则在申请人名下保存原要求编号、免修编号
//     与拒绝原因，不获得学分；
//   - 只有空字符串编号按参数错误拒绝；依据的空白口径与原文保留不变；
//   - 含前后空白的编号是合法记录，不能判为文件损坏（退出码 2）。

// waiverWhitespaceCLIRecord 构造合法记录：学生 "s1" 与 " s1 " 各自编号
// 为 r1 的要求分别指向 4 学分课程 c1 与 3 学分课程 c2，两人都没有修读。
func waiverWhitespaceCLIRecord() *diskRecord {
	return &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []diskStudent{{ID: "s1"}, {ID: " s1 "}},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
			{ID: "r1", Student: " s1 ", Course: "c2"},
		},
	}
}

// TestCLIWaiverWhitespaceStudentIDHitsExactOwner 给 " s1 " 提交依据含实际
// 文字的新申请：只能满足 " s1 " 的 r1 并获得 3 学分，提示、check 来源与
// show 历史都使用 " s1 " 实际使用的编号；"s1" 的学分、要求与申请原样。
func TestCLIWaiverWhitespaceStudentIDHitsExactOwner(t *testing.T) {
	file, _ := writeDiskRecord(t, waiverWhitespaceCLIRecord())

	out, errText, code := runCLI(t, file, "waiver", " s1 ", "r1", "w1", "学科竞赛获奖证明")
	if code != 0 {
		t.Fatalf("给 \" s1 \" 的 r1 申请免修应成功，code=%d out=%q err=%q",
			code, out, errText)
	}
	for _, want := range []string{
		"免修 w1 有效：学生  s1  的要求 r1",
		`依据 "学科竞赛获奖证明"`,
		"获得课程学分",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("成功提示应保留实际使用的完整编号与依据，缺 %q，out=%q", want, out)
		}
	}

	// " s1 " 本人：r1 满足、3 学分、来源为本人的有效免修 w1。
	out, _, code = runCLI(t, file, "check", " s1 ")
	if code != 0 {
		t.Fatalf("check \" s1 \" 应成功，code=%d out=%q", code, out)
	}
	for _, want := range []string{
		"总学分：3",
		"要求 r1（课程 c2《线性代数》，3 学分）：已满足",
		"来源为有效免修 w1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("\" s1 \" 应凭本人 r1 获得 3 学分，缺 %q，out=%q", want, out)
		}
	}
	if strings.Contains(out, "未满足要求：[") {
		t.Fatalf("\" s1 \" 的 r1 应已满足，out=%q", out)
	}

	// "s1" 保持原样：0 学分、r1 未满足、没有任何免修历史与拒绝记录。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("\"s1\" 应仍为 0 学分且 r1 未满足，code=%d out=%q", code, out)
	}
	if strings.Contains(out, "有效免修") || strings.Contains(out, "被拒绝的免修") {
		t.Fatalf("\"s1\" 名下不应出现任何免修，out=%q", out)
	}
	show, _, _ := runCLI(t, file, "show", "s1")
	if strings.Contains(show, "免修 w1") {
		t.Fatalf("申请不应记到 \"s1\" 名下，show=%q", show)
	}
	show, _, _ = runCLI(t, file, "show", " s1 ")
	if !strings.Contains(show, `免修 w1：要求 r1，依据 "学科竞赛获奖证明"，状态：有效`) {
		t.Fatalf("\" s1 \" 的 show 历史应保留实际编号与依据，show=%q", show)
	}
}

// TestCLIWaiverWhitespaceReqIDIndependent 同一学生名下 "r1" 与 " r1 " 是
// 两项不同要求：给 " r1 " 的申请只满足该完整编号对应的 3 学分要求，
// r1 仍未满足，历史中保留完整要求编号。
func TestCLIWaiverWhitespaceReqIDIndependent(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
			{ID: " r1 ", Student: "s1", Course: "c2"},
		},
	}
	file, _ := writeDiskRecord(t, d)

	out, errText, code := runCLI(t, file, "waiver", "s1", " r1 ", "w1", "外校同层次课程证明")
	if code != 0 || !strings.Contains(out, "免修 w1 有效：学生 s1 的要求  r1 ") {
		t.Fatalf("给 \" r1 \" 申请应成功并保留完整要求编号，code=%d out=%q err=%q",
			code, out, errText)
	}

	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：3") ||
		!strings.Contains(out, "未满足要求：[r1]") ||
		!strings.Contains(out, "要求  r1 （课程 c2《线性代数》，3 学分）：已满足") ||
		!strings.Contains(out, "来源为有效免修 w1") {
		t.Fatalf("只有 \" r1 \" 应满足并计 3 学分，r1 应未满足，code=%d out=%q",
			code, out)
	}
	if !strings.Contains(out, "要求 r1（课程 c1《高等数学》，4 学分）：未满足") {
		t.Fatalf("不带空白的 r1 应保持未满足，out=%q", out)
	}
	show, _, _ := runCLI(t, file, "show", "s1")
	if !strings.Contains(show, `免修 w1：要求  r1 ，依据 "外校同层次课程证明"，状态：有效`) {
		t.Fatalf("show 历史应保留完整要求编号 \" r1 \"，show=%q", show)
	}
}

// TestCLIWaiverWhitespaceWaiverIDIndependent "w1" 与 " w1 " 是不同申请
// 编号：r1 已有有效免修 w1 时，用 " w1 " 提交的新申请只能被拒绝并独立
// 保留，不能返回 w1 或按 w1 的内容报冲突；重试与冲突规则各自只命中完整
// 编号相同的那一份。
func TestCLIWaiverWhitespaceWaiverIDIndependent(t *testing.T) {
	d := &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
		Waivers: []diskWaiver{
			{ID: "w1", Student: "s1", Req: "r1", Basis: "原获奖依据", Status: "approved"},
		},
	}
	file, _ := writeDiskRecord(t, d)

	// 新编号 " w1 " 申请同一项已有有效免修的要求：退出码 1，新建拒绝记录。
	out, errText, code := runCLI(t, file, "waiver", "s1", "r1", " w1 ", "再次申请的新依据")
	if code != exitRejected {
		t.Fatalf("新编号取代已有免修应业务拒绝，code=%d out=%q err=%q", code, out, errText)
	}
	if !strings.Contains(out, "免修申请  w1  已拒绝") ||
		!strings.Contains(out, "该要求已有有效免修 w1") {
		t.Fatalf("拒绝提示应使用完整编号 \" w1 \" 并点名已有免修 w1，out=%q", out)
	}

	// check：学分来源仍是 w1，被拒绝列表单独列出 " w1 "。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为有效免修 w1") {
		t.Fatalf("r1 应仍由 w1 满足、计 4 学分，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, `免修  w1 （要求 r1，依据 "再次申请的新依据"）：该要求已有有效免修 w1`) {
		t.Fatalf("核对应单独列出被拒绝的 \" w1 \"，out=%q", out)
	}

	// show 中两份申请各自保留一行。
	show, _, _ := runCLI(t, file, "show", "s1")
	for _, want := range []string{
		`免修 w1：要求 r1，依据 "原获奖依据"，状态：有效`,
		`免修  w1 ：要求 r1，依据 "再次申请的新依据"，状态：已拒绝（该要求已有有效免修 w1）`,
	} {
		if !strings.Contains(show, want) {
			t.Fatalf("show 应分别保留 w1 与 \" w1 \" 两行，缺 %q，show=%q", want, show)
		}
	}

	// 原样重试 " w1 "：幂等返回它自己的已拒绝申请（退出码 0），不是 w1。
	out, _, code = runCLI(t, file, "waiver", "s1", "r1", " w1 ", "再次申请的新依据")
	if code != 0 || !strings.Contains(out, "免修  w1  已提交过且内容一致，返回原申请，状态：已拒绝") {
		t.Fatalf("重试 \" w1 \" 应返回其本人的原拒绝记录，code=%d out=%q", code, out)
	}

	// 对原 w1 换依据：冲突必须命中 w1（含其原依据），不能撞上 " w1 "。
	_, errText, code = runCLI(t, file, "waiver", "s1", "r1", "w1", "改换依据")
	if code != exitRejected {
		t.Fatalf("w1 换内容应冲突拒绝，code=%d err=%q", code, errText)
	}
	if !strings.Contains(errText, "免修编号 w1 已存在") ||
		!strings.Contains(errText, "原获奖依据") {
		t.Fatalf("冲突应点名原申请 w1 及其原依据，err=%q", errText)
	}
	if strings.Contains(errText, "免修编号  w1  已存在") {
		t.Fatalf("冲突不应命中带空白的 \" w1 \"，err=%q", errText)
	}
	show, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(show, `免修 w1：要求 r1，依据 "原获奖依据"，状态：有效`) {
		t.Fatalf("冲突拒绝后 w1 应保持有效且依据不变，show=%q", show)
	}
}

// TestCLIWaiverWhitespaceUnknownStudentRejected 完整学生编号不存在时按业务
// 拒绝（退出码 1）并说明学生不存在：不输出成功或已拒绝提示，不给其他
// 学生新增历史，普通空格、制表符、全角空格都不能被忽略，文件逐字节不变。
func TestCLIWaiverWhitespaceUnknownStudentRejected(t *testing.T) {
	d := &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
	}
	cases := []struct {
		label   string
		student string
	}{
		{"前导普通空格", " s1"},
		{"尾随普通空格", "s1 "},
		{"前后普通空格", " s1 "},
		{"制表符", "\ts1\t"},
		{"全角空格", "　s1　"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			file, raw := writeDiskRecord(t, d)
			out, errText, code := runCLI(t, file, "waiver", tc.student, "r1", "w1", "依据文字")
			if code != exitRejected {
				t.Fatalf("%s：应业务拒绝（退出码 1），code=%d out=%q err=%q",
					tc.label, code, out, errText)
			}
			if out != "" {
				t.Fatalf("%s：学生不存在时不应有任何业务输出，out=%q", tc.label, out)
			}
			if !strings.Contains(errText, "学生") || !strings.Contains(errText, "不存在") {
				t.Fatalf("%s：错误应说明学生不存在，err=%q", tc.label, errText)
			}
			assertFileByteIdentical(t, file, raw, tc.label+"：")

			// 不能借用 "s1" 的名义提交：其核对结果与提交前一致。
			check, _, _ := runCLI(t, file, "check", "s1")
			if !strings.Contains(check, "总学分：0") ||
				!strings.Contains(check, "未满足要求：[r1]") ||
				strings.Contains(check, "被拒绝的免修") {
				t.Fatalf("%s：\"s1\" 不应新增任何免修历史，check=%q", tc.label, check)
			}
		})
	}
}

// TestCLIWaiverWhitespaceMissingReqRejectedAndSaved 学生存在而完整要求编号
// 不存在时，即使去掉空白能找到 r1，也拒绝申请（退出码 1），但按原规则在
// 申请人名下保存原要求编号、免修编号与拒绝原因，供 check/show 核对查看，
// 不获得学分，r1 保持未满足。
func TestCLIWaiverWhitespaceMissingReqRejectedAndSaved(t *testing.T) {
	d := &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
	}
	for _, reqID := range []string{" r1", "r1 ", " r1 ", "\tr1\t", "　r1　"} {
		t.Run(fmt.Sprintf("要求编号%q", reqID), func(t *testing.T) {
			file, _ := writeDiskRecord(t, d)
			out, errText, code := runCLI(t, file, "waiver", "s1", reqID, " w1 ", "实际依据文字")
			if code != exitRejected {
				t.Fatalf("要求 %q 不存在应业务拒绝，code=%d out=%q err=%q",
					reqID, code, out, errText)
			}
			if !strings.Contains(out, "免修申请  w1  已拒绝") {
				t.Fatalf("拒绝提示应保留完整免修编号 \" w1 \"，out=%q", out)
			}

			check, _, code := runCLI(t, file, "check", "s1")
			if code != 0 || !strings.Contains(check, "总学分：0") ||
				!strings.Contains(check, "未满足要求：[r1]") {
				t.Fatalf("要求 %q 的被拒绝申请不计学分、r1 应未满足，code=%d check=%q",
					reqID, code, check)
			}
			wantLine := fmt.Sprintf("免修  w1 （要求 %s，依据 %q）：目标要求 %s 不存在或不属于该学生",
				reqID, "实际依据文字", reqID)
			if !strings.Contains(check, wantLine) {
				t.Fatalf("核对应保留完整要求编号、免修编号与原因\nwant contains %q\ncheck=%q",
					wantLine, check)
			}

			show, _, _ := runCLI(t, file, "show", "s1")
			wantShow := fmt.Sprintf("免修  w1 ：要求 %s，依据 %q，状态：已拒绝（目标要求 %s 不存在或不属于该学生）",
				reqID, "实际依据文字", reqID)
			if !strings.Contains(show, wantShow) {
				t.Fatalf("show 历史应保留原编号与原因\nwant contains %q\nshow=%q",
					wantShow, show)
			}
		})
	}
}

// TestCLIWaiverEmptyIDsRejected 只有空字符串编号按参数错误拒绝（退出码 1），
// 不创建任何申请，记录文件保持不变；全空白编号不是空字符串，仍按完整编号
// 查找并报学生不存在。
func TestCLIWaiverEmptyIDsRejected(t *testing.T) {
	file, raw := writeDiskRecord(t, waiverWhitespaceCLIRecord())
	cases := [][]string{
		{"waiver", "", "r1", "w1", "依据"},
		{"waiver", "s1", "", "w1", "依据"},
		{"waiver", "s1", "r1", "", "依据"},
	}
	for _, args := range cases {
		out, errText, code := runCLI(t, file, args...)
		if code != exitRejected {
			t.Fatalf("空字符串编号应拒绝，args=%v code=%d out=%q err=%q",
				args, code, out, errText)
		}
		if !strings.Contains(errText, "不能为空") {
			t.Fatalf("应报编号不能为空，args=%v err=%q", args, errText)
		}
		assertFileByteIdentical(t, file, raw, fmt.Sprintf("空编号 %v：", args))
	}

	// 全空白学生编号不是空字符串：按完整编号查找，学生不存在。
	out, errText, code := runCLI(t, file, "waiver", " ", "r1", "w1", "依据")
	if code != exitRejected || !strings.Contains(errText, "不存在") {
		t.Fatalf("全空白学生编号应按完整编号查找并报不存在，code=%d out=%q err=%q",
			code, out, errText)
	}
}

// TestCLIWaiverBasisWhitespacePreserved 依据不修剪：含实际文字时前后空白
// 原样保存并参与内容比较（去掉前后空白重试按内容冲突拒绝）；纯空白依据
// 仍按业务拒绝（退出码 1），拒绝历史中的依据原文逐字符保留。
func TestCLIWaiverBasisWhitespacePreserved(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "数学", "4"},
		{"req", "s1", "r1", "c1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	basis := "  学科竞赛获奖证明　"
	if out, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", basis); code != 0 {
		t.Fatalf("含实际文字的依据应成功，code=%d out=%q err=%q", code, out, errText)
	}
	show, _, _ := runCLI(t, file, "show", "s1")
	if !strings.Contains(show, fmt.Sprintf("依据 %q", basis)) {
		t.Fatalf("show 应保留依据的前后空白，want %q，show=%q", basis, show)
	}

	// 只去掉前后空白：内容不同，冲突拒绝（退出码 1），原依据保留。
	if _, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "学科竞赛获奖证明"); code != exitRejected {
		t.Fatalf("去掉前后空白的依据应按冲突拒绝，code=%d err=%q", code, errText)
	}
	show, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(show, fmt.Sprintf("依据 %q", basis)) ||
		!strings.Contains(show, "状态：有效") {
		t.Fatalf("冲突拒绝后原依据与有效状态应保留，show=%q", show)
	}

	// 纯空白依据：业务拒绝，拒绝历史保留原文与原因。
	blank := " 　\t \n "
	out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w2", blank)
	if code != exitRejected || !strings.Contains(out, "已拒绝") {
		t.Fatalf("纯空白依据应业务拒绝，code=%d out=%q", code, out)
	}
	show, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(show, fmt.Sprintf("免修 w2：要求 r1，依据 %q，状态：已拒绝（免修依据为空）", blank)) {
		t.Fatalf("被拒绝申请的空白依据原文应逐字符保留，show=%q", show)
	}
}
