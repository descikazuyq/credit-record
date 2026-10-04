package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“读取已有记录时，同一学生的同一要求只能有一份
// 有效免修”。这类冲突无法经正常申请流程落入文件（第二份申请提交时就会被
// 拒绝），所以用例直接写出结构完整、可解析的记录文件，每条断言都重新打开
// 进程访问它：
//   - 两份编号不同、状态均为有效的免修并存：无论 check、show 还是写入类
//     命令，都沿用文件内容损坏的退出码 2，不输出任何业务结果，错误点名
//     问题文件、所属学生、目标要求与两个免修编号，原文件逐字节保留；
//   - 通过修读、无关学生的合法记录都不能掩盖或分担这份冲突；
//   - 一份有效免修与已拒绝、已撤销申请共存是合法记录：核对只认有效来源、
//     学分只计一次，历史与原因完整可查，只读访问不改文件；
//   - 唯一性按学生隔离：不同学生共用要求编号与免修编号时各自正常核对。

// 以下结构仅用于在测试里拼出与磁盘格式一致的 JSON（字段标签与
// credit.fileData 保持一致）。
type diskRecord struct {
	Version       int           `json:"version"`
	Courses       []diskCourse  `json:"courses"`
	Students      []diskStudent `json:"students"`
	Requirements  []diskReq     `json:"requirements"`
	Enrollments   []diskEnr     `json:"enrollments"`
	Waivers       []diskWaiver  `json:"waivers"`
	NextResultSeq int           `json:"nextResultSeq"`
}

type diskCourse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Credit int    `json:"credit"`
	Open   bool   `json:"open"`
}
type diskStudent struct {
	ID string `json:"id"`
}
type diskReq struct {
	ID      string `json:"id"`
	Student string `json:"student"`
	Course  string `json:"course"`
}
type diskEnr struct {
	ID        string `json:"id"`
	Student   string `json:"student"`
	Req       string `json:"req"`
	Term      string `json:"term"`
	Result    string `json:"result"`
	ResultSeq int    `json:"resultSeq,omitempty"`
}
type diskWaiver struct {
	ID      string `json:"id"`
	Student string `json:"student"`
	Req     string `json:"req"`
	Basis   string `json:"basis"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
}

// writeDiskRecord 把记录序列化到临时文件，返回路径与原始字节。
func writeDiskRecord(t *testing.T, d *diskRecord) (string, []byte) {
	t.Helper()
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		t.Fatalf("序列化记录失败：%v", err)
	}
	raw = append(raw, '\n')
	file := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatalf("写入记录文件失败：%v", err)
	}
	return file, raw
}

// assertFileByteIdentical 断言记录文件与写入时的内容逐字节一致。
func assertFileByteIdentical(t *testing.T, file string, want []byte, label string) {
	t.Helper()
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("读取记录文件失败：%v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s不得改写原文件\nwant=%q\n got=%q", label, want, got)
	}
}

// assertConflictAccessRejected 用指定命令访问冲突文件：必须退出码 2、
// stdout 没有任何业务内容，stderr 点名文件、学生、要求与两个免修编号。
func assertConflictAccessRejected(t *testing.T, file string, raw []byte,
	args []string, student, req, w1, w2 string, label string) {
	t.Helper()
	out, errText, code := runCLI(t, file, args...)
	if code != exitFile {
		t.Fatalf("%s：访问含两份有效免修的记录应退出码 %d，code=%d out=%q err=%q",
			label, exitFile, code, out, errText)
	}
	if out != "" {
		t.Fatalf("%s：拒绝整份记录时不应输出任何业务结果，out=%q", label, out)
	}
	for _, want := range []string{file, "内容损坏", student, req, w1, w2} {
		if !strings.Contains(errText, want) {
			t.Fatalf("%s：错误输出应点名文件/学生 %s/要求 %s/免修 %s、%s（缺 %q），err=%q",
				label, student, req, w1, w2, want, errText)
		}
	}
	if !strings.Contains(errText, "有效免修 "+w1) ||
		!strings.Contains(errText, w2) {
		t.Fatalf("%s：错误应说明同一要求存在两份有效免修 %s 与 %s，err=%q",
			label, w1, w2, errText)
	}
	assertFileByteIdentical(t, file, raw, label+"：")
}

// conflictRecord 构造结构完整、引用齐全的记录：s1 的 r1 指向 4 学分课程 c1，
// 同一要求下有两份编号不同、依据非空的有效免修。withPass 时再附一条已通过
// 修读，用于证明通过记录不能掩盖冲突。
func conflictRecord(withPass bool) *diskRecord {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
		},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
		Waivers: []diskWaiver{
			{ID: "w1", Student: "s1", Req: "r1", Basis: "学科竞赛获奖", Status: "approved"},
			{ID: "w2", Student: "s1", Req: "r1", Basis: "外校同层次课程", Status: "approved"},
		},
	}
	if withPass {
		d.Enrollments = []diskEnr{{
			ID: "e1", Student: "s1", Req: "r1", Term: "2024春",
			Result: "passed", ResultSeq: 1,
		}}
		d.NextResultSeq = 1
	}
	return d
}

// TestCLIDuplicateApprovedWaiversInFileRejectsAllCommands 文件完整可解析，
// 但 s1 的 r1 下有两份有效免修 w1、w2：check、show、列出课程乃至写入类命令
// 都必须退出码 2，不输出核对结果或登记成功信息，错误点名四要素且文件保留。
func TestCLIDuplicateApprovedWaiversInFileRejectsAllCommands(t *testing.T) {
	file, raw := writeDiskRecord(t, conflictRecord(false))

	// 只读核对：不能挑其中一份免修继续核对，也不能出现总学分。
	assertConflictAccessRejected(t, file, raw,
		[]string{"check", "s1"}, "s1", "r1", "w1", "w2", "check 当事学生")

	// 历史查看同样拒绝：不能展示任何一份看似正常的免修。
	assertConflictAccessRejected(t, file, raw,
		[]string{"show", "s1"}, "s1", "r1", "w1", "w2", "show 当事学生")

	// 与冲突无关的只读命令也要在加载阶段整份拒绝。
	assertConflictAccessRejected(t, file, raw,
		[]string{"list-courses"}, "s1", "r1", "w1", "w2", "list-courses")

	// 写入类命令：不能报告登记成功，更不能把冲突文件覆盖成“干净”的新记录。
	assertConflictAccessRejected(t, file, raw,
		[]string{"student", "s9"}, "s1", "r1", "w1", "w2", "写入类命令 student")

	// 针对另一名尚未出现在文件里的学生核对：文件整体不可读，仍退出码 2。
	assertConflictAccessRejected(t, file, raw,
		[]string{"check", "ghost"}, "s1", "r1", "w1", "w2", "check 其他学生")

	assertFileByteIdentical(t, file, raw, "全部访问后：")
}

// TestCLIDuplicateApprovedWaiversNotMaskedByPassedEnrollment 即使目标要求
// 另有通过修读（本来足以满足要求），两份有效免修的冲突仍必须以退出码 2
// 拒绝整份记录：不能拿着通过结果输出学分核对，文件原样保留。
func TestCLIDuplicateApprovedWaiversNotMaskedByPassedEnrollment(t *testing.T) {
	file, raw := writeDiskRecord(t, conflictRecord(true))

	out, errText, code := runCLI(t, file, "check", "s1")
	if code != exitFile {
		t.Fatalf("通过修读不能掩盖免修冲突，code=%d out=%q err=%q", code, out, errText)
	}
	if out != "" || strings.Contains(out, "总学分") || strings.Contains(out, "通过修读") {
		t.Fatalf("冲突记录不得输出任何学分核对结果，out=%q", out)
	}
	if !strings.Contains(errText, "内容损坏") || !strings.Contains(errText, "w1") ||
		!strings.Contains(errText, "w2") || !strings.Contains(errText, "r1") {
		t.Fatalf("错误仍应点名 r1 下冲突的 w1/w2，err=%q", errText)
	}
	assertFileByteIdentical(t, file, raw, "带通过修读的冲突文件：")
}

// TestCLIDuplicateApprovedWaiversRejectsEntireFileForOtherStudent 文件里
// 另有一名完全合法的学生 s2（自己的课程、自己的要求 r1、自己的一份有效
// 免修 w1）：s1 名下的冲突仍然让整份文件不可读，连查询 s2 都退出码 2，
// 错误归属 s1 而不是把 s2 的同号记录误报成重复取代。
func TestCLIDuplicateApprovedWaiversRejectsEntireFileForOtherStudent(t *testing.T) {
	d := conflictRecord(false)
	d.Courses = append(d.Courses, diskCourse{ID: "c2", Name: "线性代数", Credit: 3, Open: true})
	d.Students = append(d.Students, diskStudent{ID: "s2"})
	d.Requirements = append(d.Requirements, diskReq{ID: "r1", Student: "s2", Course: "c2"})
	d.Waivers = append(d.Waivers, diskWaiver{
		ID: "w1", Student: "s2", Req: "r1", Basis: "s2 本人的依据", Status: "approved",
	})
	file, raw := writeDiskRecord(t, d)

	// 查询完全合法的 s2 也必须整份拒绝：唯一性冲突破坏的是整份文件。
	out, errText, code := runCLI(t, file, "check", "s2")
	if code != exitFile || out != "" {
		t.Fatalf("s1 的冲突应让整份文件不可读，连 s2 都查不了，code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(errText, "s1") || !strings.Contains(errText, "r1") ||
		!strings.Contains(errText, "w1") || !strings.Contains(errText, "w2") {
		t.Fatalf("错误应归属 s1 的 r1 与 w1/w2，err=%q", errText)
	}
	if strings.Contains(errText, "学生 s2") {
		t.Fatalf("s2 只有一份有效免修，不应被点名为冲突方，err=%q", errText)
	}
	assertFileByteIdentical(t, file, raw, "含无关合法学生的冲突文件：")
}

// mixedWaiverRecord 构造同一要求 r1 下一份有效免修 wa、一份已拒绝 wr、
// 一份已撤销 wv 的合法记录，历史排列由调用方给定。
func mixedWaiverRecord(t *testing.T, order []string) (*diskRecord, map[string]diskWaiver) {
	t.Helper()
	byID := map[string]diskWaiver{
		"wa": {ID: "wa", Student: "s1", Req: "r1", Basis: "外校同层次课程", Status: "approved"},
		"wr": {ID: "wr", Student: "s1", Req: "r1", Basis: "再次申请的新依据",
			Status: "rejected", Reason: "该要求已有有效免修 wa"},
		"wv": {ID: "wv", Student: "s1", Req: "r1", Basis: "原竞赛材料",
			Status: "revoked", Reason: "材料无法核实"},
	}
	d := &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
	}
	for _, id := range order {
		d.Waivers = append(d.Waivers, byID[id])
	}
	return d, byID
}

// TestCLIMixedWaiverHistoryOpensWithSingleValidSource 同一要求下一份有效免修
// 与已拒绝、已撤销申请共存的记录必须能正常打开：核对只以有效免修 wa 说明
// 来源、4 学分只计一次；check 与 show 都完整保留三份申请的编号、依据、状态
// 及原有拒绝/撤销原因，失效申请不重新参与满足；历史排在有效申请前后结论
// 相同；只读访问不改文件。
func TestCLIMixedWaiverHistoryOpensWithSingleValidSource(t *testing.T) {
	for name, order := range map[string][]string{
		"有效申请在最前": {"wa", "wr", "wv"},
		"有效申请在最后": {"wr", "wv", "wa"},
	} {
		t.Run(name, func(t *testing.T) {
			d, _ := mixedWaiverRecord(t, order)
			file, raw := writeDiskRecord(t, d)

			out, _, code := runCLI(t, file, "check", "s1")
			if code != 0 {
				t.Fatalf("合法混合历史应正常核对，code=%d out=%q", code, out)
			}
			for _, want := range []string{
				"总学分：4",
				"要求 r1（课程 c1《高等数学》，4 学分）：已满足",
				"来源为有效免修 wa",
				"被拒绝的免修：",
				`免修 wr（要求 r1，依据 "再次申请的新依据"）：该要求已有有效免修 wa`,
				"已撤销免修：[wv]",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("核对结果应包含 %q，out=%q", want, out)
				}
			}
			if strings.Contains(out, "未满足要求：[") {
				t.Fatalf("r1 应由有效免修满足，不应列为未满足，out=%q", out)
			}
			// 只能有一份有效来源：wa 只出现一次，失效申请不能被描述成来源。
			if strings.Count(out, "来源为有效免修 wa") != 1 {
				t.Fatalf("有效免修来源应只计一次，out=%q", out)
			}

			// show 保留三份申请各自的编号、依据、状态与括号中的原有原因。
			show, _, code := runCLI(t, file, "show", "s1")
			if code != 0 {
				t.Fatalf("show 合法混合历史应成功，code=%d out=%q", code, show)
			}
			for _, want := range []string{
				`免修 wa：要求 r1，依据 "外校同层次课程"，状态：有效`,
				`免修 wr：要求 r1，依据 "再次申请的新依据"，状态：已拒绝（该要求已有有效免修 wa）`,
				`免修 wv：要求 r1，依据 "原竞赛材料"，状态：已撤销（材料无法核实）`,
			} {
				if !strings.Contains(show, want) {
					t.Fatalf("历史查看应完整保留 %q，out=%q", want, show)
				}
			}
			// 三份申请所在行各只出现一次，不能为去重丢掉历史或重复列出。
			for _, line := range []string{
				"免修 wa：要求 r1",
				"免修 wr：要求 r1",
				"免修 wv：要求 r1",
			} {
				if strings.Count(show, line) != 1 {
					t.Fatalf("show 中 %q 应恰好出现一次，out=%q", line, show)
				}
			}

			// 只读访问不得改动合法记录。
			assertFileByteIdentical(t, file, raw, "check/show 之后：")
		})
	}
}

// TestCLIInvalidOnlyWaiverHistoryLeavesRequirementUnmet 文件里只有已拒绝与
// 已撤销申请（没有有效免修）时记录合法、可正常打开，但要求必须未满足、
// 学分为零，两份历史及原因仍可在 check/show 中查到。
func TestCLIInvalidOnlyWaiverHistoryLeavesRequirementUnmet(t *testing.T) {
	d := &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
		Waivers: []diskWaiver{
			{ID: "wr", Student: "s1", Req: "r1", Basis: "材料不全",
				Status: "rejected", Reason: "免修依据为空"},
			{ID: "wv", Student: "s1", Req: "r1", Basis: "旧竞赛获奖",
				Status: "revoked", Reason: "材料无法核实"},
		},
	}
	file, raw := writeDiskRecord(t, d)

	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("仅失效申请的记录应正常核对，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：0") || !strings.Contains(out, "未满足要求：[r1]") ||
		strings.Contains(out, "来源为有效免修") {
		t.Fatalf("失效申请不能满足要求或成为来源，out=%q", out)
	}
	if !strings.Contains(out, "免修 wr") || !strings.Contains(out, "免修依据为空") ||
		!strings.Contains(out, "已撤销免修：[wv]") {
		t.Fatalf("被拒绝与已撤销历史及原因仍应列出，out=%q", out)
	}
	show, _, _ := runCLI(t, file, "show", "s1")
	if !strings.Contains(show, "状态：已拒绝（免修依据为空）") ||
		!strings.Contains(show, "状态：已撤销（材料无法核实）") {
		t.Fatalf("show 应保留两份失效申请的状态与原因，out=%q", show)
	}
	assertFileByteIdentical(t, file, raw, "只读查询之后：")
}

// TestCLISameReqAndWaiverIDAcrossStudentsFromFile 唯一性只作用于同一学生
// 名下的同一要求：两名学生各自使用相同的要求编号 r1、相同的免修编号 w1，
// 只要各自只有一份有效申请就正常读取；两人的要求指向不同学分课程时，
// 核对分别得到本人的课程学分与免修来源，不能误报重复取代。
func TestCLISameReqAndWaiverIDAcrossStudentsFromFile(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []diskStudent{{ID: "s1"}, {ID: "s2"}},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
			{ID: "r1", Student: "s2", Course: "c2"},
		},
		Waivers: []diskWaiver{
			{ID: "w1", Student: "s1", Req: "r1", Basis: "s1 的竞赛获奖", Status: "approved"},
			{ID: "w1", Student: "s2", Req: "r1", Basis: "s2 的外校修读", Status: "approved"},
		},
	}
	file, raw := writeDiskRecord(t, d)

	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("s1 的合法记录应正常核对，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "要求 r1（课程 c1《高等数学》，4 学分）：已满足") ||
		!strings.Contains(out, "来源为有效免修 w1") ||
		strings.Contains(out, "未满足要求：[") {
		t.Fatalf("s1 应凭本人 w1 获得本人课程的 4 学分，out=%q", out)
	}

	out, _, code = runCLI(t, file, "check", "s2")
	if code != 0 {
		t.Fatalf("s2 的合法记录应正常核对，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：3") ||
		!strings.Contains(out, "要求 r1（课程 c2《线性代数》，3 学分）：已满足") ||
		!strings.Contains(out, "来源为有效免修 w1") {
		t.Fatalf("s2 应凭本人 w1 获得本人课程的 3 学分，不能误报重复取代，out=%q", out)
	}

	// 历史各归各：show 中只能看到本人依据，不能串到另一人。
	show1, _, _ := runCLI(t, file, "show", "s1")
	if !strings.Contains(show1, `免修 w1：要求 r1，依据 "s1 的竞赛获奖"，状态：有效`) ||
		strings.Contains(show1, "s2 的外校修读") {
		t.Fatalf("s1 的历史应只含本人 w1 与依据，out=%q", show1)
	}
	show2, _, _ := runCLI(t, file, "show", "s2")
	if !strings.Contains(show2, `免修 w1：要求 r1，依据 "s2 的外校修读"，状态：有效`) ||
		strings.Contains(show2, "s1 的竞赛获奖") {
		t.Fatalf("s2 的历史应只含本人 w1 与依据，out=%q", show2)
	}

	assertFileByteIdentical(t, file, raw, "跨学生同号合法记录只读之后：")
}
