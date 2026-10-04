package main

import (
	"strings"
	"testing"
)

// 本文件从命令行入口回归“读取已有成绩记录时，两次独立的成绩提交不能共用
// 同一个结果提交序号”。这类冲突无法经正常提交流程落入文件（序号由计数器
// 递增分配），所以用例直接写出结构完整、可解析的记录文件，每条断言都重新
// 打开进程访问它：
//   - 两条不同的已提交修读共用同一个正整数序号时，无论两条都通过还是一条
//     未通过、无论它们属于同一学生的重复修读还是不同学生的独立修读，check、
//     show、list-courses 乃至写入类命令都沿用记录文件错误的退出码 2：不输出
//     任何业务结果，错误点名问题文件与重复的提交序号，原文件逐字节保留；
//   - 即使文件里另有一名完全正常的学生，也不能只读入正常部分继续核对；
//   - 修读编号相同、学期相同、记录在文件里的排列顺序都不是冲突依据；
//   - 合法记录中核对仍以提交序号较小的通过修读说明来源，多次通过只计一次
//     学分，更早提交的未通过保留在历史中但不成为来源；
//   - 多条尚未提交结果的选课共用序号 0 合法且不得学分；已提交序号允许跳号；
//     不同学生共用修读编号但序号不同时各自保留所属记录；只读访问不改文件。

// assertSeqDupAccessRejected 用指定命令访问含重复提交序号的记录文件：
// 必须退出码 2、stdout 没有任何业务内容，stderr 点名问题文件、说明内容
// 损坏并给出重复的提交序号。调用方在全部访问后再统一比对原文件字节。
func assertSeqDupAccessRejected(t *testing.T, file string, args []string, seq, label string) {
	t.Helper()
	out, errText, code := runCLI(t, file, args...)
	if code != exitFile {
		t.Fatalf("%s：访问含重复提交序号的记录应退出码 %d，code=%d out=%q err=%q",
			label, exitFile, code, out, errText)
	}
	if out != "" {
		t.Fatalf("%s：整份记录损坏时不应输出任何业务结果，out=%q", label, out)
	}
	for _, want := range []string{
		file,
		"内容损坏",
		"结果提交序号 " + seq + " 重复",
	} {
		if !strings.Contains(errText, want) {
			t.Fatalf("%s：错误输出应点名问题文件并给出重复序号（缺 %q），err=%q",
				label, want, errText)
		}
	}
}

// seqDupBase 构造结构完整、引用齐全的基础记录：s1 的要求 r1 指向 4 学分
// 课程 c1。修读列表由各用例自行拼入。
func seqDupBase() *diskRecord {
	return &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
	}
}

func enrRecord(id, student, req, term, result string, seq int) diskEnr {
	return diskEnr{
		ID: id, Student: student, Req: req, Term: term,
		Result: result, ResultSeq: seq,
	}
}

// TestCLIDuplicateResultSeqRejectsAllCommands 文件完整可解析、引用齐全，
// 但同一学生同一学期的两条不同修读（两次通过）共用同一个正整数提交序号：
// 所有只读与写入类命令都必须退出码 2，不输出业务结果，错误点名文件与序号，
// 原文件逐字节保留。
func TestCLIDuplicateResultSeqRejectsAllCommands(t *testing.T) {
	d := seqDupBase()
	d.Enrollments = []diskEnr{
		enrRecord("e1", "s1", "r1", "2024春", "passed", 3),
		enrRecord("e2", "s1", "r1", "2024春", "passed", 3),
	}
	d.NextResultSeq = 3
	file, raw := writeDiskRecord(t, d)

	// 只读核对：不能挑其中一条通过记录继续核对，也不能出现总学分。
	assertSeqDupAccessRejected(t, file, []string{"check", "s1"}, "3", "check 当事学生")
	// 历史查看同样拒绝。
	assertSeqDupAccessRejected(t, file, []string{"show", "s1"}, "3", "show 当事学生")
	// 与冲突修读无关的只读命令也要在加载阶段整份拒绝。
	assertSeqDupAccessRejected(t, file, []string{"list-courses"}, "3", "list-courses")
	// 写入类命令：不能报告登记成功，更不能把冲突文件覆盖成“干净”的新记录。
	assertSeqDupAccessRejected(t, file, []string{"student", "s9"}, "3", "写入类命令 student")
	// 针对文件里根本不存在的学生核对：文件整体不可读，仍退出码 2。
	assertSeqDupAccessRejected(t, file, []string{"check", "ghost"}, "3", "check 其他学生")

	assertFileByteIdentical(t, file, raw, "全部访问后：")
}

// TestCLIDuplicateResultSeqOnePassedOneFailedRejected 两条同序号修读一条
// 通过、一条未通过时同样整份损坏：未通过也是一次独立的结果提交，不能因为
// 它不计学分就放过序号冲突。
func TestCLIDuplicateResultSeqOnePassedOneFailedRejected(t *testing.T) {
	d := seqDupBase()
	d.Enrollments = []diskEnr{
		// 文件排列刻意把未通过放前面，证明判定不依赖排列顺序。
		enrRecord("e2", "s1", "r1", "2024秋", "failed", 5),
		enrRecord("e1", "s1", "r1", "2024春", "passed", 5),
	}
	d.NextResultSeq = 5
	file, raw := writeDiskRecord(t, d)

	out, errText, code := runCLI(t, file, "check", "s1")
	if code != exitFile || out != "" {
		t.Fatalf("一通过一未通过共用序号也应整份拒绝，code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(errText, "结果提交序号 5 重复") || !strings.Contains(errText, file) {
		t.Fatalf("错误应点名问题文件与重复序号 5，err=%q", errText)
	}
	assertFileByteIdentical(t, file, raw, "一通过一未通过冲突文件：")
}

// TestCLIDuplicateResultSeqAcrossStudentsRejectsEntireFile 不同学生的独立
// 修读共用同一提交序号（修读编号与学期都相同也一样）时整份损坏；文件里
// 另有一名记录完全正常的学生 s3，也不能只读入 s3 继续核对。写入类命令
// 同样退出码 2，原文件保留。
func TestCLIDuplicateResultSeqAcrossStudentsRejectsEntireFile(t *testing.T) {
	d := seqDupBase()
	d.Courses = append(d.Courses, diskCourse{ID: "c2", Name: "线性代数", Credit: 3, Open: true})
	d.Students = append(d.Students, diskStudent{ID: "s2"}, diskStudent{ID: "s3"})
	d.Requirements = append(d.Requirements,
		diskReq{ID: "r1", Student: "s2", Course: "c1"},
		diskReq{ID: "r9", Student: "s3", Course: "c2"},
	)
	d.Enrollments = []diskEnr{
		// s1 与 s2 的同编号、同学期修读各自合法，但两条提交共用序号 2。
		enrRecord("e1", "s1", "r1", "2024春", "passed", 2),
		enrRecord("e1", "s2", "r1", "2024春", "failed", 2),
		// s3 的记录完全正常：自己的课程、自己的要求、自己的序号 1。
		enrRecord("e7", "s3", "r9", "2024春", "passed", 1),
	}
	d.NextResultSeq = 2
	file, raw := writeDiskRecord(t, d)

	// 查询完全合法的 s3 也必须整份拒绝：序号冲突破坏的是整份文件，
	// 不能只读入正常部分继续核对。
	out, errText, code := runCLI(t, file, "check", "s3")
	if code != exitFile || out != "" {
		t.Fatalf("s1/s2 的序号冲突应让整份文件不可读，连 s3 都查不了，code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(errText, "内容损坏") || !strings.Contains(errText, file) ||
		!strings.Contains(errText, "结果提交序号 2 重复") {
		t.Fatalf("错误应点名问题文件与重复序号 2，err=%q", errText)
	}
	// 不能泄露 s3 的核对结论。
	if strings.Contains(out, "总学分") {
		t.Fatalf("整份拒绝时不得输出 s3 的学分，out=%q", out)
	}

	// 写入类命令同样在加载阶段失败，不能覆盖异常原文件。
	out, errText, code = runCLI(t, file, "student", "s8")
	if code != exitFile || strings.Contains(out, "已登记学生") {
		t.Fatalf("写入类命令遇序号冲突应退出码 %d 且不登记，code=%d out=%q err=%q",
			exitFile, code, out, errText)
	}
	assertFileByteIdentical(t, file, raw, "含无关合法学生的冲突文件：")
}

// TestCLIEarliestResultSeqSourceFromReorderedFile 直接写一份排列与提交先后
// 不一致的合法记录：后提交的通过记录排在文件前面，修读编号与学期先后也与
// 提交先后相反。核对仍以提交序号较小的通过修读说明来源，两次通过只计一次
// 学分，只读访问不改文件。
func TestCLIEarliestResultSeqSourceFromReorderedFile(t *testing.T) {
	d := seqDupBase()
	d.Enrollments = []diskEnr{
		// 按文件排列/编号/学期都会挑到 e1，只有提交序号指向 e2。
		enrRecord("e1", "s1", "r1", "2024春", "passed", 2),
		enrRecord("e2", "s1", "r1", "2024秋", "passed", 1),
	}
	d.NextResultSeq = 2
	file, raw := writeDiskRecord(t, d)

	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("序号各不相同的合法记录应正常核对，code=%d out=%q", code, out)
	}
	for _, want := range []string{
		"总学分：4",
		"要求 r1（课程 c1《高等数学》，4 学分）：已满足",
		"来源为通过修读 e2",
		"共通过 2 次，仅计一次学分",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("应以序号较小的 e2 说明来源且两次通过只计一次（缺 %q），out=%q", want, out)
		}
	}
	if strings.Contains(out, "来源为通过修读 e1") || strings.Contains(out, "未满足要求：[") {
		t.Fatalf("不能按文件排列/编号/学期挑到 e1，也不应有未满足要求，out=%q", out)
	}

	// show 中两条通过记录都保留，结果与学期原样。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 应成功，code=%d out=%q", code, show)
	}
	if !strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：通过") ||
		!strings.Contains(show, "修读 e2：要求 r1，学期 2024秋，结果：通过") {
		t.Fatalf("两条通过记录都应保留在历史中，out=%q", show)
	}

	assertFileByteIdentical(t, file, raw, "check/show 之后：")
}

// TestCLIEarlierFailedSubmissionStaysHistoryNotSource 经正常命令行流程产生
// “先未通过、后两次通过”的历史（每次调用都重新打开文件，覆盖落盘再加载）：
// 核对以先提交的那条通过说明来源，更早的未通过保留在历史中、不成为来源，
// 总学分仍是一份 4 学分。
func TestCLIEarlierFailedSubmissionStaysHistoryNotSource(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		// 同一学期三条修读：重复修读合法，靠编号区分。
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"enroll", "s1", "r1", "2024春", "e2"},
		{"enroll", "s1", "r1", "2024春", "e3"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}
	// 提交次序：e3 先未通过（序号 1），e2 再通过（序号 2），e1 最后通过（序号 3）。
	for _, st := range [][]string{
		{"fail", "s1", "e3"},
		{"pass", "s1", "e2"},
		{"pass", "s1", "e1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对应成功，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：4") || strings.Contains(out, "未满足要求：[") {
		t.Fatalf("要求应满足、总学分 4，out=%q", out)
	}
	if !strings.Contains(out, "来源为通过修读 e2") ||
		!strings.Contains(out, "共通过 2 次，仅计一次学分") {
		t.Fatalf("来源应是先提交通过的 e2（不是更早的未通过 e3，也不是后提交的 e1），out=%q", out)
	}
	if strings.Contains(out, "来源为通过修读 e3") || strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("未通过 e3 与后提交的 e1 都不能成为来源，out=%q", out)
	}

	// 未通过记录保留在历史中，结果不变。
	show, _, _ := runCLI(t, file, "show", "s1")
	if !strings.Contains(show, "修读 e3：要求 r1，学期 2024春，结果：未通过") ||
		!strings.Contains(show, "修读 e2：要求 r1，学期 2024春，结果：通过") ||
		!strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：通过") {
		t.Fatalf("三条修读结果都应保留（e3 未通过、e2/e1 通过），out=%q", show)
	}

	// 只读核对幂等：再次核对不改动文件。
	before := mustReadRecord(t, file)
	if _, _, code := runCLI(t, file, "check", "s1"); code != 0 {
		t.Fatal("重复核对应成功")
	}
	assertRecordUnchanged(t, file, before, "重复核对：")
}

// TestCLIMultipleEnrolledZeroSeqLegalAndWorthNoCredit 只有选课、尚未提交
// 结果的修读统一使用序号 0：同一学期多条选课、跨学生并存都合法，不能因
// 共享 0 而报重复，也不能由此获得学分；只读访问不改文件。
func TestCLIMultipleEnrolledZeroSeqLegalAndWorthNoCredit(t *testing.T) {
	d := seqDupBase()
	d.Students = append(d.Students, diskStudent{ID: "s2"})
	d.Requirements = append(d.Requirements, diskReq{ID: "r1", Student: "s2", Course: "c1"})
	d.Enrollments = []diskEnr{
		// 尚未提交结果的修读按现有格式不带 resultSeq（即 0）。
		{ID: "e1", Student: "s1", Req: "r1", Term: "2024春", Result: "enrolled"},
		{ID: "e2", Student: "s1", Req: "r1", Term: "2024春", Result: "enrolled"},
		{ID: "e3", Student: "s1", Req: "r1", Term: "2024秋", Result: "enrolled"},
		// 跨学生共用修读编号，同样没有提交序号。
		{ID: "e1", Student: "s2", Req: "r1", Term: "2024春", Result: "enrolled"},
	}
	d.NextResultSeq = 0
	file, raw := writeDiskRecord(t, d)

	for _, student := range []string{"s1", "s2"} {
		out, _, code := runCLI(t, file, "check", student)
		if code != 0 {
			t.Fatalf("学生 %s 的多条选课记录应正常读取，code=%d out=%q", student, code, out)
		}
		if !strings.Contains(out, "总学分：0") ||
			!strings.Contains(out, "未满足要求：[r1]") ||
			strings.Contains(out, "通过修读") {
			t.Fatalf("学生 %s 的选课不应获得学分或出现通过来源，out=%q", student, out)
		}
	}
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 || strings.Count(show, "结果：选课") != 3 {
		t.Fatalf("s1 应有三条选课记录，code=%d out=%q", code, show)
	}
	show2, _, code := runCLI(t, file, "show", "s2")
	if code != 0 || strings.Count(show2, "结果：选课") != 1 ||
		strings.Contains(show2, "修读 e2") || strings.Contains(show2, "修读 e3") {
		t.Fatalf("s2 只应有本人的一条选课，不能看到 s1 的记录，out=%q", show2)
	}
	assertFileByteIdentical(t, file, raw, "多条选课只读之后：")
}

// TestCLISubmittedSeqGapsNeedNotBeConsecutive 已提交序号允许有间隔：跳号
// 记录正常读取与核对，来源仍是通过记录中序号最小的一条；未通过不成为来源，
// 选课记录不携带序号；只读访问不改文件。
func TestCLISubmittedSeqGapsNeedNotBeConsecutive(t *testing.T) {
	d := seqDupBase()
	d.Enrollments = []diskEnr{
		enrRecord("e1", "s1", "r1", "2024春", "failed", 1),
		enrRecord("e2", "s1", "r1", "2024夏", "passed", 5), // 序号 2/3/4 缺失
		enrRecord("e3", "s1", "r1", "2024秋", "passed", 9), // 继续跳号
		{ID: "e4", Student: "s1", Req: "r1", Term: "2025春", Result: "enrolled"},
	}
	d.NextResultSeq = 9
	file, raw := writeDiskRecord(t, d)

	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("跳号的合法记录应正常核对，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e2") ||
		!strings.Contains(out, "共通过 2 次，仅计一次学分") ||
		strings.Contains(out, "未满足要求：[") {
		t.Fatalf("跳号不应影响核对：来源应是通过中序号最小的 e2、两次通过只计一次，out=%q", out)
	}
	if strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("序号 1 的未通过不能成为来源，out=%q", out)
	}
	show, _, _ := runCLI(t, file, "show", "s1")
	if !strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：未通过") ||
		!strings.Contains(show, "修读 e4：要求 r1，学期 2025春，结果：选课") {
		t.Fatalf("未通过与选课记录都应原样保留，out=%q", show)
	}
	assertFileByteIdentical(t, file, raw, "跳号记录只读之后：")
}

// TestCLISameEnrollmentIDAcrossStudentsDifferentSeq 不同学生共用修读编号与
// 学期、但提交序号不同：各自的记录正常读取、核对各归本人，不能因编号或
// 学期相同误报冲突或串记录；文件排列与提交先后不一致时结论不变。
func TestCLISameEnrollmentIDAcrossStudentsDifferentSeq(t *testing.T) {
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
			// 后提交的 s2 记录排在文件前面：来源只看本人的提交序号。
			enrRecord("e1", "s2", "r1", "2024春", "passed", 2),
			enrRecord("e1", "s1", "r1", "2024春", "passed", 1),
		},
		NextResultSeq: 2,
	}
	file, raw := writeDiskRecord(t, d)

	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("s1 的合法记录应正常核对，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "要求 r1（课程 c1《高等数学》，4 学分）：已满足") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("s1 应凭本人 e1 获得本人 4 学分课程，out=%q", out)
	}

	out, _, code = runCLI(t, file, "check", "s2")
	if code != 0 {
		t.Fatalf("s2 的合法记录应正常核对，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：3") ||
		!strings.Contains(out, "要求 r1（课程 c2《线性代数》，3 学分）：已满足") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("s2 应凭本人 e1 获得本人 3 学分课程，编号相同不构成冲突，out=%q", out)
	}

	// show 各归各：只能看到本人的修读与课程，不能串到另一人。
	show1, _, _ := runCLI(t, file, "show", "s1")
	if !strings.Contains(show1, "修读 e1：要求 r1，学期 2024春，结果：通过") ||
		strings.Contains(show1, "线性代数") {
		t.Fatalf("s1 的历史应只含本人修读与本人课程，out=%q", show1)
	}
	show2, _, _ := runCLI(t, file, "show", "s2")
	if !strings.Contains(show2, "修读 e1：要求 r1，学期 2024春，结果：通过") ||
		strings.Contains(show2, "高等数学") {
		t.Fatalf("s2 的历史应只含本人修读与本人课程，out=%q", show2)
	}

	assertFileByteIdentical(t, file, raw, "跨学生同号不同序号只读之后：")
}
