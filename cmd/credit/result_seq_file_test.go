package main

import (
	"strconv"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“读取已有记录时，两次独立的成绩提交不能共用同一个
// 结果提交序号”。序号冲突无法经正常提交流程落入文件（序号由计数器单调
// 分配），所以用例直接写出结构完整、可解析的记录文件，每条断言都重新
// 打开进程访问它：
//   - 两条已提交修读共用同一个正整数序号：无论两条都通过还是一条未通过、
//     无论属于同一学生的重复修读还是不同学生各自的独立修读，check、show、
//     列出课程乃至写入类命令都沿用记录文件错误的退出码 2，不输出任何业务
//     结果，错误点名问题文件与重复的提交序号，原文件逐字节保留；
//   - 另一名学生完全正常的记录不能让文件被部分读入；
//   - 只有选课的修读使用序号 0，多条并存合法且不得学分；
//   - 合法记录正常核对：文件排列、修读编号与学期次序与提交先后不一致时，
//     仍以提交序号最小的通过修读说明来源；序号允许有间隔；不同学生共用
//     修读编号但序号不同时各自核对，只读访问不改文件。

// seqDupRecord 构造结构完整、引用齐全但序号损坏的记录：s1 的要求 r1
// 指向 4 学分课程 c1，两条已提交修读共用同一个序号。result2 取 "passed"
// 或 "failed"，用于证明两种结果组合都判损坏。
func seqDupRecord(seq int, result2 string) *diskRecord {
	return &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
		Enrollments: []diskEnr{
			{ID: "e1", Student: "s1", Req: "r1", Term: "2024春",
				Result: "passed", ResultSeq: seq},
			{ID: "e2", Student: "s1", Req: "r1", Term: "2024秋",
				Result: result2, ResultSeq: seq},
		},
		NextResultSeq: seq,
	}
}

// assertSeqConflictRejected 用指定命令访问序号冲突文件：必须退出码 2、
// stdout 没有任何业务内容，stderr 点名问题文件与重复的提交序号。
func assertSeqConflictRejected(t *testing.T, file string, raw []byte,
	args []string, seq int, label string) {
	t.Helper()
	out, errText, code := runCLI(t, file, args...)
	if code != exitFile {
		t.Fatalf("%s：访问共用结果提交序号 %d 的记录应退出码 %d，code=%d out=%q err=%q",
			label, seq, exitFile, code, out, errText)
	}
	if out != "" {
		t.Fatalf("%s：拒绝整份记录时不应输出任何业务结果，out=%q", label, out)
	}
	for _, want := range []string{file, "内容损坏", "结果提交序号", "重复", strconv.Itoa(seq)} {
		if !strings.Contains(errText, want) {
			t.Fatalf("%s：错误输出应点名问题文件与重复的提交序号 %d（缺 %q），err=%q",
				label, seq, want, errText)
		}
	}
	assertFileByteIdentical(t, file, raw, label+"：")
}

// TestCLIDuplicateResultSeqRejectsAllCommands 文件完整可解析、其余内容
// 合法，但 s1 的两条通过修读共用结果提交序号 1：check、show、列出课程乃至
// 写入类命令都必须退出码 2，不输出核对结果或登记成功信息，错误点名文件与
// 重复序号，原文件逐字节保留。
func TestCLIDuplicateResultSeqRejectsAllCommands(t *testing.T) {
	file, raw := writeDiskRecord(t, seqDupRecord(1, "passed"))

	// 只读核对：不能挑其中一条通过记录继续核对，也不能出现总学分。
	assertSeqConflictRejected(t, file, raw, []string{"check", "s1"}, 1, "check 当事学生")

	// 历史查看同样拒绝：不能展示任何一条看似正常的修读。
	assertSeqConflictRejected(t, file, raw, []string{"show", "s1"}, 1, "show 当事学生")

	// 与冲突无关的只读命令也要在加载阶段整份拒绝。
	assertSeqConflictRejected(t, file, raw, []string{"list-courses"}, 1, "list-courses")

	// 写入类命令：不能报告登记成功，更不能把冲突文件覆盖成“干净”的新记录。
	assertSeqConflictRejected(t, file, raw, []string{"student", "s9"}, 1, "写入类命令 student")

	// 针对另一名尚未出现在文件里的学生核对：文件整体不可读，仍退出码 2。
	assertSeqConflictRejected(t, file, raw, []string{"check", "ghost"}, 1, "check 其他学生")

	assertFileByteIdentical(t, file, raw, "全部访问后：")
}

// TestCLIDuplicateResultSeqPassAndFail 一条通过与一条未通过共用同一序号，
// 不能因为结果不同或学期不同就当成两次独立提交：仍以退出码 2 整份拒绝。
func TestCLIDuplicateResultSeqPassAndFail(t *testing.T) {
	file, raw := writeDiskRecord(t, seqDupRecord(3, "failed"))

	out, errText, code := runCLI(t, file, "check", "s1")
	if code != exitFile {
		t.Fatalf("通过与未通过共用序号也应退出码 %d，code=%d out=%q err=%q",
			exitFile, code, out, errText)
	}
	if out != "" {
		t.Fatalf("冲突记录不得输出任何核对结果，out=%q", out)
	}
	if !strings.Contains(errText, "结果提交序号") || !strings.Contains(errText, "3") {
		t.Fatalf("错误应指出重复的提交序号 3，err=%q", errText)
	}
	assertFileByteIdentical(t, file, raw, "通过/未通过冲突文件：")
}

// TestCLIDuplicateResultSeqAcrossStudents 两名学生使用相同的修读编号、相同
// 学期（编号只在学生名下唯一，这本属合法），但两条通过修读共用同一个
// 全局提交序号：仍判整份损坏；即使只查询其中一人，也不能读入。
func TestCLIDuplicateResultSeqAcrossStudents(t *testing.T) {
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
		Enrollments: []diskEnr{
			{ID: "e1", Student: "s1", Req: "r1", Term: "2024春",
				Result: "passed", ResultSeq: 2},
			{ID: "e1", Student: "s2", Req: "r1", Term: "2024春",
				Result: "passed", ResultSeq: 2},
		},
		NextResultSeq: 2,
	}
	file, raw := writeDiskRecord(t, d)

	for _, student := range []string{"s1", "s2", "ghost"} {
		out, errText, code := runCLI(t, file, "check", student)
		if code != exitFile || out != "" {
			t.Fatalf("跨学生序号冲突应整份不可读（查询 %s），code=%d out=%q err=%q",
				student, code, out, errText)
		}
		if !strings.Contains(errText, "内容损坏") || !strings.Contains(errText, "2") ||
			!strings.Contains(errText, file) {
			t.Fatalf("错误应点名文件与重复序号 2，err=%q", errText)
		}
	}
	assertFileByteIdentical(t, file, raw, "跨学生冲突文件：")
}

// TestCLIDuplicateResultSeqRejectsEntireFileForOtherStudent 文件里另有一名
// 完全合法的学生 s2（自己的课程、要求与一条独立序号的通过修读）：s1 名下
// 的序号冲突仍让整份文件不可读，连查询 s2 都退出码 2，不能只读入正常部分
// 继续核对；错误指出 s1 冲突的序号，而不是 s2 的合法序号。
func TestCLIDuplicateResultSeqRejectsEntireFileForOtherStudent(t *testing.T) {
	d := seqDupRecord(2, "failed")
	d.Courses = append(d.Courses, diskCourse{ID: "c2", Name: "线性代数", Credit: 3, Open: true})
	d.Students = append(d.Students, diskStudent{ID: "s2"})
	d.Requirements = append(d.Requirements, diskReq{ID: "r1", Student: "s2", Course: "c2"})
	d.Enrollments = append(d.Enrollments, diskEnr{
		ID: "e1", Student: "s2", Req: "r1", Term: "2024春",
		Result: "passed", ResultSeq: 8,
	})
	d.NextResultSeq = 8
	file, raw := writeDiskRecord(t, d)

	out, errText, code := runCLI(t, file, "check", "s2")
	if code != exitFile || out != "" {
		t.Fatalf("s1 的序号冲突应让整份文件不可读，连 s2 都查不了，code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(errText, "内容损坏") || !strings.Contains(errText, "结果提交序号 2") {
		t.Fatalf("错误应指出 s1 冲突的序号 2，err=%q", errText)
	}
	assertFileByteIdentical(t, file, raw, "含无关合法学生的冲突文件：")
}

// TestCLILegalOutOfOrderResultSeqChecksByEarliestSubmission 合法记录：
// s1 的一项 4 学分要求有两次通过修读，文件把后提交的记录放在前面，
// 修读编号与学期次序也与提交先后不同；另有一条更早提交的未通过修读和
// 两条只有选课的修读。核对必须以提交序号最小的通过修读说明来源，两次
// 通过都保留、只计一次 4 学分，未通过与选课记录留在历史中。
func TestCLILegalOutOfOrderResultSeqChecksByEarliestSubmission(t *testing.T) {
	d := &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
		// 文件排列刻意与提交先后不一致：先放后提交（序号 5）的 e1，
		// 再放更早提交（序号 1）的未通过 e3，选课 e4 夹在中间，
		// 最先提交的通过修读 e2（序号 2）排在后面，e5 收尾。
		Enrollments: []diskEnr{
			{ID: "e1", Student: "s1", Req: "r1", Term: "2024春",
				Result: "passed", ResultSeq: 5},
			{ID: "e3", Student: "s1", Req: "r1", Term: "2023秋",
				Result: "failed", ResultSeq: 1},
			{ID: "e4", Student: "s1", Req: "r1", Term: "2024春", Result: "enrolled"},
			{ID: "e2", Student: "s1", Req: "r1", Term: "2025秋",
				Result: "passed", ResultSeq: 2},
			{ID: "e5", Student: "s1", Req: "r1", Term: "2024秋", Result: "enrolled"},
		},
		// 2 与 5 之间有间隔：已提交序号不要求连续。
		NextResultSeq: 5,
	}
	file, raw := writeDiskRecord(t, d)

	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("序号合法的乱序记录应正常核对，code=%d out=%q", code, out)
	}
	for _, want := range []string{
		"总学分：4",
		"要求 r1（课程 c1《高等数学》，4 学分）：已满足",
		"来源为通过修读 e2", // 序号最小的通过，而不是文件里最先出现的 e1
		"共通过 2 次",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("核对结果应包含 %q，out=%q", want, out)
		}
	}
	if strings.Contains(out, "未满足要求：[") {
		t.Fatalf("要求 r1 应已满足，out=%q", out)
	}
	// 更早提交的未通过修读与只有选课的修读不能成为来源。
	if strings.Contains(out, "来源为通过修读 e1") || strings.Contains(out, "来源为通过修读 e3") {
		t.Fatalf("来源不能是后提交的 e1 或未通过的 e3，out=%q", out)
	}

	// show 保留全部五条修读及其结果，文件排列不变。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 应成功，code=%d out=%q", code, show)
	}
	for _, want := range []string{
		"修读 e1：要求 r1，学期 2024春，结果：通过",
		"修读 e2：要求 r1，学期 2025秋，结果：通过",
		"修读 e3：要求 r1，学期 2023秋，结果：未通过",
		"修读 e4：要求 r1，学期 2024春，结果：选课",
		"修读 e5：要求 r1，学期 2024秋，结果：选课",
	} {
		if !strings.Contains(show, want) {
			t.Fatalf("show 应保留全部修读及结果（缺 %q），out=%q", want, show)
		}
	}

	// 只读访问不得改动合法记录。
	assertFileByteIdentical(t, file, raw, "check/show 之后：")
}

// TestCLIEnrolledSharingSeqZeroIsLegal 只有选课、尚未提交成绩的修读使用
// 序号 0：同一学生同一学期的多条选课、不同学生共用编号的多条选课同时
// 存在都合法，不能因共享 0 报重复，也不能由此获得学分；它们与真正提交
// 过的通过结果并存时核对结论不变。
func TestCLIEnrolledSharingSeqZeroIsLegal(t *testing.T) {
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
		Enrollments: []diskEnr{
			// s1 同一学期两条选课（编号不同），都没有结果序号。
			{ID: "e1", Student: "s1", Req: "r1", Term: "2024春", Result: "enrolled"},
			{ID: "e2", Student: "s1", Req: "r1", Term: "2024春", Result: "enrolled"},
			// s2 的同编号、同学期选课：跨学生共享编号与序号 0 同样合法。
			{ID: "e1", Student: "s2", Req: "r1", Term: "2024春", Result: "enrolled"},
			// s2 另有一条真正通过的修读，序号 1。
			{ID: "e2", Student: "s2", Req: "r1", Term: "2024秋",
				Result: "passed", ResultSeq: 1},
		},
		NextResultSeq: 1,
	}
	file, raw := writeDiskRecord(t, d)

	// s1：只有选课，0 学分、要求未满足。
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") || strings.Contains(out, "通过修读") {
		t.Fatalf("s1 只有选课应 0 学分且要求未满足，code=%d out=%q", code, out)
	}

	// s2：选课不影响另一条通过，仍只得自己的 3 学分，来源是 e2。
	out, _, code = runCLI(t, file, "check", "s2")
	if code != 0 || !strings.Contains(out, "总学分：3") ||
		!strings.Contains(out, "来源为通过修读 e2") ||
		strings.Contains(out, "未满足要求：[") {
		t.Fatalf("s2 应凭本人通过修读 e2 获得 3 学分，code=%d out=%q", code, out)
	}

	// show 中两人的选课记录各自保留、互不串扰。
	show1, _, _ := runCLI(t, file, "show", "s1")
	if strings.Count(show1, "结果：选课") != 2 || strings.Contains(show1, "通过") {
		t.Fatalf("s1 应只有本人两条选课记录，out=%q", show1)
	}
	show2, _, _ := runCLI(t, file, "show", "s2")
	if !strings.Contains(show2, "修读 e1：要求 r1，学期 2024春，结果：选课") ||
		!strings.Contains(show2, "修读 e2：要求 r1，学期 2024秋，结果：通过") {
		t.Fatalf("s2 应保留本人的选课与通过各一条，out=%q", show2)
	}

	assertFileByteIdentical(t, file, raw, "选课共享序号 0 的合法记录只读之后：")
}

// TestCLISharedEnrollmentIDDistinctSeqChecksSeparately 不同学生共用修读
// 编号、要求编号与学期，但结果提交序号各不相同时记录合法（序号允许有
// 间隔）：两人的结果与学分来源各归各，核对互不串扰，只读访问不改文件。
func TestCLISharedEnrollmentIDDistinctSeqChecksSeparately(t *testing.T) {
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
		Enrollments: []diskEnr{
			// 两人共用编号 e1、同学期、同编号要求，但序号不同（1 与 4）。
			{ID: "e1", Student: "s1", Req: "r1", Term: "2024春",
				Result: "passed", ResultSeq: 1},
			{ID: "e1", Student: "s2", Req: "r1", Term: "2024春",
				Result: "failed", ResultSeq: 4},
			// 两人的 e2 也共用编号，序号继续不同（9 与 12，均留间隔）。
			{ID: "e2", Student: "s1", Req: "r1", Term: "2024秋",
				Result: "passed", ResultSeq: 9},
			{ID: "e2", Student: "s2", Req: "r1", Term: "2024秋",
				Result: "passed", ResultSeq: 12},
		},
		NextResultSeq: 12,
	}
	file, raw := writeDiskRecord(t, d)

	// s1：e1、e2 都通过，同一要求多次通过只计 4 学分，来源是序号 1 的 e1。
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") || !strings.Contains(out, "共通过 2 次") ||
		strings.Contains(out, "未满足要求：[") {
		t.Fatalf("s1 应凭本人最早通过的 e1 获得 4 学分，code=%d out=%q", code, out)
	}

	// s2：e1 未通过、e2 通过，只计自己课程 c2 的 3 学分，来源是 e2；
	// 未通过的 e1 保留但不成为来源。
	out, _, code = runCLI(t, file, "check", "s2")
	if code != 0 || !strings.Contains(out, "总学分：3") ||
		!strings.Contains(out, "来源为通过修读 e2") ||
		strings.Contains(out, "来源为通过修读 e1") ||
		strings.Contains(out, "未满足要求：[") {
		t.Fatalf("s2 应只凭本人通过修读 e2 获得 3 学分，code=%d out=%q", code, out)
	}

	// show 各归各：s1 两条通过，s2 一条未通过一条通过。
	show1, _, _ := runCLI(t, file, "show", "s1")
	if strings.Count(show1, "结果：通过") != 2 || strings.Contains(show1, "未通过") {
		t.Fatalf("s1 应只有本人两条通过记录，out=%q", show1)
	}
	show2, _, _ := runCLI(t, file, "show", "s2")
	if !strings.Contains(show2, "修读 e1：要求 r1，学期 2024春，结果：未通过") ||
		!strings.Contains(show2, "修读 e2：要求 r1，学期 2024秋，结果：通过") {
		t.Fatalf("s2 应保留本人的未通过 e1 与通过 e2，out=%q", show2)
	}

	assertFileByteIdentical(t, file, raw, "跨学生同编号不同序号合法记录只读之后：")
}
