package main

import (
	"strings"
	"testing"
)

// 本文件从命令行入口回归“打开记录文件时，已撤销免修也应保留一份曾经有效的
// 申请所必需的信息”。残缺的已撤销记录无法经正常申请-撤销流程落入文件，
// 因此用例直接写出结构完整、可解析的记录文件，每条断言都重新调用入口：
//   - 已撤销免修缺目标要求（编号空/不存在/同号只属他人）或原依据为空白时，
//     无论 check、show、list-courses 还是写入类命令，无论查询的是不是问题
//     记录所属学生，都沿用文件内容损坏的退出码 2，不输出任何业务结果，
//     错误点名记录文件、所属学生与免修编号，并区分两类原因；
//   - 通过修读不能掩盖、另一名完全合法学生的存在不能分担这份损坏；
//   - 合法的已撤销历史正常展示，原编号、依据、撤销原因与顺序保持原样，
//     不重新提供学分；同一要求可有多份已撤销 + 一份有效，失效历史不纳入
//     有效免修的唯一性限制；
//   - 被拒绝申请因要求不存在或依据空白留下的记录与原因是正常历史，check
//     与 show 都不应误报损坏。

// revokedCLIBase 构造结构完整、引用齐全的记录：s1 的 r1 指向 4 学分课程 c1。
func revokedCLIBase() *diskRecord {
	return &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
	}
}

// assertCLIRevokedCorruptAccess 用指定命令访问含残缺已撤销记录的文件：
// 必须退出码 2、stdout 无业务内容，stderr 点名文件、学生、免修并说明原因。
func assertCLIRevokedCorruptAccess(t *testing.T, file string, raw []byte,
	args []string, student, waiver, kind, label string) {
	t.Helper()
	out, errText, code := runCLI(t, file, args...)
	if code != exitFile {
		t.Fatalf("%s：访问含残缺已撤销免修的记录应退出码 %d，code=%d out=%q err=%q",
			label, exitFile, code, out, errText)
	}
	if out != "" {
		t.Fatalf("%s：拒绝整份记录时不应输出任何业务结果，out=%q", label, out)
	}
	for _, want := range []string{file, "内容损坏", student, waiver, kind} {
		if !strings.Contains(errText, want) {
			t.Fatalf("%s：错误输出应含文件/学生 %s/免修 %s/原因（缺 %q），err=%q",
				label, student, waiver, want, errText)
		}
	}
	assertFileByteIdentical(t, file, raw, label+"：")
}

// TestCLIRevokedWaiverMissingRequirementRejectsAllCommands 已撤销免修指向不
// 存在要求的记录：check/show/list-courses/写入类命令都退出码 2，且连查询
// 另一名学生也一样；错误区分“目标要求无效”，文件原样保留。
func TestCLIRevokedWaiverMissingRequirementRejectsAllCommands(t *testing.T) {
	d := revokedCLIBase()
	// 另加一名完全合法的学生 s2：自己的课程 c2、要求 r9、通过修读 e9。
	d.Courses = append(d.Courses, diskCourse{ID: "c2", Name: "线性代数", Credit: 3, Open: true})
	d.Students = append(d.Students, diskStudent{ID: "s2"})
	d.Requirements = append(d.Requirements, diskReq{ID: "r9", Student: "s2", Course: "c2"})
	d.Enrollments = []diskEnr{{
		ID: "e9", Student: "s2", Req: "r9", Term: "2024春",
		Result: "passed", ResultSeq: 1,
	}}
	d.NextResultSeq = 1
	d.Waivers = []diskWaiver{
		{ID: "wv", Student: "s1", Req: "rX", Basis: "原竞赛材料",
			Status: "revoked", Reason: "材料无法核实"},
	}
	file, raw := writeDiskRecord(t, d)

	assertCLIRevokedCorruptAccess(t, file, raw,
		[]string{"check", "s1"}, "s1", "wv", "目标要求无效", "check 问题学生")
	assertCLIRevokedCorruptAccess(t, file, raw,
		[]string{"show", "s1"}, "s1", "wv", "目标要求无效", "show 问题学生")
	assertCLIRevokedCorruptAccess(t, file, raw,
		[]string{"list-courses"}, "s1", "wv", "目标要求无效", "list-courses")
	assertCLIRevokedCorruptAccess(t, file, raw,
		[]string{"student", "s9"}, "s1", "wv", "目标要求无效", "写入类命令 student")
	// 即使本次只查询完全合法、与问题记录无关的 s2，也必须退出码 2。
	assertCLIRevokedCorruptAccess(t, file, raw,
		[]string{"check", "s2"}, "s1", "wv", "目标要求无效", "check 其他学生 s2")
	assertCLIRevokedCorruptAccess(t, file, raw,
		[]string{"show", "s2"}, "s1", "wv", "目标要求无效", "show 其他学生 s2")

	assertFileByteIdentical(t, file, raw, "全部访问后：")
}

// TestCLIRevokedWaiverBlankBasisRejectsAllCommands 已撤销免修的依据为空串或
// 全为空白时整份文件不可读：各类命令退出码 2，错误说明“原依据为空”。
func TestCLIRevokedWaiverBlankBasisRejectsAllCommands(t *testing.T) {
	for _, basis := range []string{"", "   ", "\t", "\r\n", " \t \n  "} {
		d := revokedCLIBase()
		d.Waivers = []diskWaiver{{
			ID: "wv", Student: "s1", Req: "r1", Basis: basis,
			Status: "revoked", Reason: "材料无法核实",
		}}
		file, raw := writeDiskRecord(t, d)
		assertCLIRevokedCorruptAccess(t, file, raw,
			[]string{"check", "s1"}, "s1", "wv", "原依据为空", "check")
		assertCLIRevokedCorruptAccess(t, file, raw,
			[]string{"show", "s1"}, "s1", "wv", "原依据为空", "show")
		assertCLIRevokedCorruptAccess(t, file, raw,
			[]string{"waiver", "s1", "r1", "w2", "新申请依据"},
			"s1", "wv", "原依据为空", "写入类命令 waiver")
	}
}

// TestCLIRevokedWBlankReqIDRejects 已撤销免修的要求编号为空字符串时同样按
// 目标要求无效拒绝。
func TestCLIRevokedWBlankReqIDRejects(t *testing.T) {
	d := revokedCLIBase()
	d.Waivers = []diskWaiver{{
		ID: "wv", Student: "s1", Req: "", Basis: "原竞赛材料",
		Status: "revoked", Reason: "材料无法核实",
	}}
	file, raw := writeDiskRecord(t, d)
	assertCLIRevokedCorruptAccess(t, file, raw,
		[]string{"check", "s1"}, "s1", "wv", "目标要求无效", "check")
}

// TestCLIRevokedWaiverCrossStudentReqNotBorrowed 同号要求只存在于另一名学生
// s2 名下时，不能借给 s1 的已撤销历史：整份文件退出码 2，错误归属 s1。
func TestCLIRevokedWaiverCrossStudentReqNotBorrowed(t *testing.T) {
	d := revokedCLIBase()
	// 去掉 s1 自己的 r1，只在 s2 名下保留 r1。
	d.Requirements = nil
	d.Courses = append(d.Courses, diskCourse{ID: "c2", Name: "线性代数", Credit: 3, Open: true})
	d.Students = append(d.Students, diskStudent{ID: "s2"})
	d.Requirements = append(d.Requirements, diskReq{ID: "r1", Student: "s2", Course: "c2"})
	d.Waivers = []diskWaiver{
		{ID: "w1", Student: "s1", Req: "r1", Basis: "s1 的原依据",
			Status: "revoked", Reason: "材料无法核实"},
	}
	file, raw := writeDiskRecord(t, d)

	out, errText, code := runCLI(t, file, "check", "s2")
	if code != exitFile || out != "" {
		t.Fatalf("s1 失去要求归属的已撤销历史应让整份文件不可读，code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(errText, "s1") || !strings.Contains(errText, "w1") ||
		!strings.Contains(errText, "目标要求无效") {
		t.Fatalf("错误应归属 s1 的 w1 并说明目标要求无效，err=%q", errText)
	}
	if strings.Contains(errText, "学生 s2 的已撤销") {
		t.Fatalf("s2 没有问题记录，不应被点名，err=%q", errText)
	}
	assertFileByteIdentical(t, file, raw, "跨学生借要求的损坏文件：")
}

// TestCLILegitRevokedHistoryDisplays 合法的已撤销历史正常读取和展示：同一
// 要求保留两份已撤销 + 一份有效，check 只认有效来源、4 学分只计一次；
// show 逐条保留原编号、依据、状态与撤销原因，顺序与文件一致；只读访问
// 不改文件。

func TestCLILegitRevokedHistoryDisplays(t *testing.T) {
	d := revokedCLIBase()
	d.Waivers = []diskWaiver{
		{ID: "wv1", Student: "s1", Req: "r1", Basis: "第一次材料",
			Status: "revoked", Reason: "材料无法核实"},
		{ID: "wa", Student: "s1", Req: "r1", Basis: "外校同层次课程",
			Status: "approved"},
		{ID: "wv2", Student: "s1", Req: "r1", Basis: "  第二次材料\n",
			Status: "revoked", Reason: "手动撤销"},
	}
	file, raw := writeDiskRecord(t, d)

	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("合法已撤销历史应正常核对，code=%d out=%q", code, out)
	}
	for _, want := range []string{
		"总学分：4",
		"来源为有效免修 wa",
		"已撤销免修：[wv1 wv2]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("核对结果应包含 %q，out=%q", want, out)
		}
	}
	if strings.Count(out, "来源为有效免修 wa") != 1 {
		t.Fatalf("只应由有效免修计一次学分，out=%q", out)
	}

	// show：三份历史各按原样、原顺序列出，依据中的空白也保留。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 应成功，code=%d out=%q", code, show)
	}
	for _, want := range []string{
		`免修 wv1：要求 r1，依据 "第一次材料"，状态：已撤销（材料无法核实）`,
		`免修 wa：要求 r1，依据 "外校同层次课程"，状态：有效`,
		`免修 wv2：要求 r1，依据 "  第二次材料\n"，状态：已撤销（手动撤销）`,
	} {
		if !strings.Contains(show, want) {
			t.Fatalf("show 应保留 %q，out=%q", want, show)
		}
	}
	lines := []string{"免修 wv1：", "免修 wa：", "免修 wv2："}
	var positions []int
	for _, l := range lines {
		positions = append(positions, strings.Index(show, l))
	}
	if positions[0] < 0 || positions[1] < 0 || positions[2] < 0 ||
		!(positions[0] < positions[1] && positions[1] < positions[2]) {
		t.Fatalf("历史顺序应与文件一致（wv1, wa, wv2），positions=%v out=%q", positions, show)
	}
	assertFileByteIdentical(t, file, raw, "只读展示之后：")
}

// TestCLIRevokedHistoryNoCreditWithoutValidWaiver 只有已撤销历史、没有有效
// 免修也没有通过修读时：要求未满足、0 学分，已撤销列表正常列出。
func TestCLIRevokedHistoryNoCreditWithoutValidWaiver(t *testing.T) {
	d := revokedCLIBase()
	d.Waivers = []diskWaiver{
		{ID: "wv", Student: "s1", Req: "r1", Basis: "旧材料",
			Status: "revoked", Reason: "材料无法核实"},
	}
	file, _ := writeDiskRecord(t, d)
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("合法已撤销历史应正常核对，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：0") || !strings.Contains(out, "未满足要求：[r1]") ||
		!strings.Contains(out, "已撤销免修：[wv]") || strings.Contains(out, "来源为有效免修") {
		t.Fatalf("已撤销历史不重新提供学分，out=%q", out)
	}
}

// TestCLIRejectedWaiverGapsAreNormalHistory 被拒绝的申请因要求不存在或依据
// 空白留下的记录与原拒绝原因是正常历史：不能按已撤销的检查条件误报文件
// 损坏，check 与 show 都应成功并展示。
func TestCLIRejectedWaiverGapsAreNormalHistory(t *testing.T) {
	d := revokedCLIBase()
	d.Waivers = []diskWaiver{
		{ID: "wj1", Student: "s1", Req: "rX", Basis: "找不到要求的依据",
			Status: "rejected", Reason: "目标要求 rX 不存在或不属于该学生"},
		{ID: "wj2", Student: "s1", Req: "r1", Basis: "",
			Status: "rejected", Reason: "免修依据为空"},
		{ID: "wj3", Student: "s1", Req: "r1", Basis: " \t\n ",
			Status: "rejected", Reason: "免修依据为空"},
	}
	file, raw := writeDiskRecord(t, d)

	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("被拒绝的残缺申请是正常历史，check 应成功，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：0") || !strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("被拒绝历史不满足要求，out=%q", out)
	}
	for _, want := range []string{
		"被拒绝的免修：",
		`免修 wj1（要求 rX，依据 "找不到要求的依据"）：目标要求 rX 不存在或不属于该学生`,
		`免修 wj2（要求 r1，依据 ""）：免修依据为空`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("核对结果应保留拒绝历史 %q，out=%q", want, out)
		}
	}

	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 正常拒绝历史应成功，code=%d out=%q", code, show)
	}
	if !strings.Contains(show, "状态：已拒绝（目标要求 rX 不存在或不属于该学生）") ||
		!strings.Contains(show, `免修 wj2：要求 r1，依据 ""，状态：已拒绝（免修依据为空）`) {
		t.Fatalf("show 应保留原拒绝记录与原因，out=%q", show)
	}
	assertFileByteIdentical(t, file, raw, "只读查询之后：")
}

// TestCLIRevokedWaiverBasisWithTextKeepsWhitespace 已撤销依据含实际文字时，
// 周围空白是材料的一部分，正常打开且 show 用 %q 原样展示，不被裁剪。
func TestCLIRevokedWaiverBasisWithTextKeepsWhitespace(t *testing.T) {
	d := revokedCLIBase()
	d.Waivers = []diskWaiver{
		{ID: "wv", Student: "s1", Req: "r1", Basis: "  学科竞赛\t获奖\n材料  ",
			Status: "revoked", Reason: "材料无法核实"},
	}
	file, _ := writeDiskRecord(t, d)
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("含空白的实际依据应正常展示，code=%d out=%q", code, show)
	}
	if !strings.Contains(show, `依据 "  学科竞赛\t获奖\n材料  "`) {
		t.Fatalf("依据中的空白应原样展示，out=%q", show)
	}
}
