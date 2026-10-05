package main

import (
	"strings"
	"testing"
)

// 本文件从命令行入口回归“enroll 按用户给出的完整学生编号、要求编号和
// 修读编号确定修读归属”，与成绩提交、免修功能对完整编号的区分保持一致。
// 记录文件可以合法保存带前后空白（普通空格、制表符、全角空格）的编号，
// 选课登记也必须如此：
//   - 给 " s1 " 登记的修读只出现在这名学生名下，新修读保留完整编号、
//     初始为选课，成功提示与 show 中的修读归属一致；"s1" 原有的修读、
//     学分和要求满足情况保持原样；
//   - 同一学生名下 "e1" 与 " e1 " 是两份独立修读，可在同一学期指向同一
//     要求；登记其中一份不能返回另一份已有记录，也不能借用另一份的结果；
//   - 完整修读编号、要求、学期相同时重复登记返回原记录（不新增、不重置
//     成绩），同一完整编号改用其他要求或学期仍拒绝；
//   - 课程停开后，已有相同修读的重复登记继续返回原记录，真正新增修读仍
//     拒绝；
//   - 完整学生编号不存在时明确报告该学生不存在，学生存在但完整要求编号
//     不在本人名下时明确报告本人名下没有该要求：退出码 1，不显示登记
//     成功，不新增记录、不改写原文件；即使去掉空白能找到其他对象，也
//     不能据此登记；
//   - 空字符串编号仍拒绝，新修读编号只有空白时仍不得建立；学期继续沿用
//     既有输入规则（只去首尾空白）。

// enrollWhitespaceCLIRecord 构造合法记录：学生 "s1" 与 " s1 " 各自的
// 要求 r1 指向 4 学分开放课程 c1，两人名下都没有修读。
func enrollWhitespaceCLIRecord() *diskRecord {
	return &diskRecord{
		Version: 1,
		Courses: []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students: []diskStudent{
			{ID: "s1"},
			{ID: " s1 "},
		},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
			{ID: "r1", Student: " s1 ", Course: "c1"},
		},
	}
}

// TestCLIEnrollWhitespaceStudentIDHitsExactOwner 给 " s1 " 选课：成功
// 提示与 show 都把修读 e1 归在 " s1 " 名下，编号与学期原样保留、初始
// 为选课；"s1" 名下仍无修读，学分与要求满足情况保持原样。提交通过后
// 学分也只计入 " s1 " 本人。
func TestCLIEnrollWhitespaceStudentIDHitsExactOwner(t *testing.T) {
	file, _ := writeDiskRecord(t, enrollWhitespaceCLIRecord())

	out, errText, code := runCLI(t, file, "enroll", " s1 ", "r1", "2024春", "e1")
	if code != 0 {
		t.Fatalf("给 \" s1 \" 选课应成功，code=%d out=%q err=%q", code, out, errText)
	}
	// 成功提示必须使用完整学生编号，且初始状态为选课。
	for _, want := range []string{"已登记修读 e1", "学生  s1 ", "要求 r1", "学期 2024春", "状态：选课"} {
		if !strings.Contains(out, want) {
			t.Fatalf("成功提示应包含 %q，out=%q", want, out)
		}
	}

	// show 中修读只挂在 " s1 " 名下，归属与成功提示一致。
	show, _, code := runCLI(t, file, "show", " s1 ")
	if code != 0 || !strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：选课") {
		t.Fatalf("\" s1 \" 的 show 应列出本人新修读且初始为选课，code=%d show=%q",
			code, show)
	}
	showS1, _, _ := runCLI(t, file, "show", "s1")
	if strings.Contains(showS1, "修读 e1") || !strings.Contains(showS1, "修读：（无）") {
		t.Fatalf("修读不应出现在 \"s1\" 名下，show=%q", showS1)
	}

	// 提交通过后：只有 " s1 " 获得 4 学分，"s1" 仍为 0 学分、r1 未满足。
	if _, errText, code := runCLI(t, file, "pass", " s1 ", "e1"); code != 0 {
		t.Fatalf("\" s1 \" 的 e1 提交通过应成功，code=%d err=%q", code, errText)
	}
	out, _, code = runCLI(t, file, "check", " s1 ")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("\" s1 \" 应凭本人 e1 获得 4 学分，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("\"s1\" 应仍为 0 学分且 r1 未满足，code=%d out=%q", code, out)
	}

	// 落盘记录中新建修读挂在完整编号学生名下，编号原文未被修剪。
	assertFileContains(t, file, `"student": " s1 "`)
	rec := mustReadRecord(t, file)
	if !strings.Contains(rec, `"id": "e1"`) {
		t.Fatalf("落盘记录应包含新修读 e1，实际=%s", rec)
	}
}

// TestCLIEnrollWhitespaceEnrollmentIDIndependent 同一学生名下 "e1"（已
// 通过）与 " e1 " 是两份独立修读：登记 " e1 " 必须新建、初始为选课，
// 不能返回 "e1" 或借用其通过结果；两份修读在同一学期指向同一要求并存，
// 学分来源仍是原通过修读 "e1"。
func TestCLIEnrollWhitespaceEnrollmentIDIndependent(t *testing.T) {
	d := &diskRecord{
		Version:  1,
		Courses:  []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students: []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
		},
		Enrollments: []diskEnr{
			{ID: "e1", Student: "s1", Req: "r1", Term: "2024春", Result: "passed", ResultSeq: 1},
		},
		NextResultSeq: 1,
	}
	file, _ := writeDiskRecord(t, d)

	out, errText, code := runCLI(t, file, "enroll", "s1", "r1", "2024春", " e1 ")
	if code != 0 || !strings.Contains(out, "已登记修读  e1 ") ||
		!strings.Contains(out, "状态：选课") {
		t.Fatalf("\" e1 \" 应作为独立修读新建且初始为选课，code=%d out=%q err=%q",
			code, out, errText)
	}

	show, _, _ := runCLI(t, file, "show", "s1")
	if !strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：通过") ||
		!strings.Contains(show, "修读  e1 ：要求 r1，学期 2024春，结果：选课") {
		t.Fatalf("两份修读应各自保留结果并存，show=%q", show)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("学分来源应仍是原通过修读 \"e1\"，code=%d out=%q", code, out)
	}
}

// TestCLIEnrollWhitespaceIdempotentAndConflictRules 完整编号、要求与学期
// 完全相同的重复登记返回原记录（文件不改写、成绩不重置）；同一完整编号
// 改用其他要求或学期退出码 1 并保留原记录。" e1 " 的幂等与冲突只命中
// 带空白的那一份，不与 "e1" 混。
func TestCLIEnrollWhitespaceIdempotentAndConflictRules(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []diskStudent{{ID: " s1 "}},
		Requirements: []diskReq{
			{ID: "r1", Student: " s1 ", Course: "c1"},
			{ID: "r2", Student: " s1 ", Course: "c2"},
		},
		Enrollments: []diskEnr{
			{ID: "e1", Student: " s1 ", Req: "r1", Term: "2024春", Result: "passed", ResultSeq: 1},
		},
		NextResultSeq: 1,
	}
	file, raw := writeDiskRecord(t, d)

	// 完整内容一致的重复登记：幂等返回原记录（仍为通过），文件逐字节不变。
	out, errText, code := runCLI(t, file, "enroll", " s1 ", "r1", "2024春", "e1")
	if code != 0 || !strings.Contains(out, "修读 e1 已存在且内容一致，返回原记录（状态：通过）") {
		t.Fatalf("完整一致的重复登记应幂等返回原记录，code=%d out=%q err=%q",
			code, out, errText)
	}
	assertFileByteIdentical(t, file, raw, "幂等重复登记：")

	// 同一完整编号换要求、换学期：退出码 1，原记录与文件保持原样。
	for _, args := range [][]string{
		{"enroll", " s1 ", "r2", "2024春", "e1"},
		{"enroll", " s1 ", "r1", "2024秋", "e1"},
	} {
		out, errText, code := runCLI(t, file, args...)
		if code != exitRejected {
			t.Fatalf("同编号换要求/学期应业务拒绝，args=%v code=%d out=%q err=%q",
				args, code, out, errText)
		}
		if !strings.Contains(errText, "修读编号 e1") {
			t.Fatalf("冲突错误应点名修读编号 e1，err=%q", errText)
		}
		assertFileByteIdentical(t, file, raw, "同编号换内容：")
	}
	show, _, _ := runCLI(t, file, "show", " s1 ")
	if !strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：通过") {
		t.Fatalf("冲突拒绝后原修读的要求、学期与通过结果应保留，show=%q", show)
	}

	// 带空白修读编号的幂等只命中带空白的那一份：名下只有 " e1 " 时，
	// 原内容重复返回原记录且文件不变，换学期仍拒绝。
	d2 := &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
		Enrollments: []diskEnr{
			{ID: " e1 ", Student: "s1", Req: "r1", Term: "2024秋", Result: "enrolled"},
		},
	}
	file2, raw2 := writeDiskRecord(t, d2)
	out, errText, code = runCLI(t, file2, "enroll", "s1", "r1", "2024秋", " e1 ")
	if code != 0 || !strings.Contains(out, "修读  e1  已存在且内容一致，返回原记录（状态：选课）") {
		t.Fatalf("\" e1 \" 原内容重复登记应幂等命中本人，code=%d out=%q err=%q",
			code, out, errText)
	}
	assertFileByteIdentical(t, file2, raw2, "带空白编号幂等重复：")
	if _, errText, code := runCLI(t, file2, "enroll", "s1", "r1", "2025春", " e1 "); code != exitRejected {
		t.Fatalf("\" e1 \" 换学期登记应业务拒绝，code=%d err=%q", code, errText)
	}
	assertFileByteIdentical(t, file2, raw2, "带空白编号换学期：")
}

// TestCLIEnrollWhitespaceClosedCourse 课程停开后：已有相同修读的重复登记
// 继续返回原记录（退出码 0、文件不变）；真正新增修读退出码 1 并报停开，
// 不能借去空白碰上另一名学生绕开限制。
func TestCLIEnrollWhitespaceClosedCourse(t *testing.T) {
	d := &diskRecord{
		Version:  1,
		Courses:  []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: false}},
		Students: []diskStudent{{ID: "s1"}, {ID: " s1 "}},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
			{ID: "r1", Student: " s1 ", Course: "c1"},
		},
		Enrollments: []diskEnr{
			{ID: "e1", Student: " s1 ", Req: "r1", Term: "2024春", Result: "enrolled"},
		},
	}
	file, raw := writeDiskRecord(t, d)

	// 已有相同修读的重复登记：仍幂等返回原记录，文件不变。
	out, errText, code := runCLI(t, file, "enroll", " s1 ", "r1", "2024春", "e1")
	if code != 0 || !strings.Contains(out, "返回原记录（状态：选课）") {
		t.Fatalf("停开后已有修读重复登记应返回原记录，code=%d out=%q err=%q",
			code, out, errText)
	}
	assertFileByteIdentical(t, file, raw, "停开后幂等重复：")

	// 真正新增修读（含带空白的新修读编号）：退出码 1 并明确报停开。
	for _, args := range [][]string{
		{"enroll", " s1 ", "r1", "2024秋", "e2"},
		{"enroll", " s1 ", "r1", "2024春", " e1 "},
		{"enroll", "s1", "r1", "2024秋", "e2"},
	} {
		out, errText, code := runCLI(t, file, args...)
		if code != exitRejected || !strings.Contains(errText, "停开") {
			t.Fatalf("停开课程下新增修读 %v 应拒绝并报停开，code=%d out=%q err=%q",
				args, code, out, errText)
		}
		assertFileByteIdentical(t, file, raw, "停开后新增：")
	}

	// 原归属保持不变：只有 " s1 " 名下有 e1。
	show, _, _ := runCLI(t, file, "show", " s1 ")
	if !strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：选课") {
		t.Fatalf("\" s1 \" 的原 e1 应保留，show=%q", show)
	}
	showS1, _, _ := runCLI(t, file, "show", "s1")
	if strings.Contains(showS1, "修读 e") {
		t.Fatalf("\"s1\" 名下不应出现修读，show=%q", showS1)
	}
}

// TestCLIEnrollWhitespaceNotFoundRejected 完整编号找不到对象时退出码 1：
// 明确报告学生不存在或本人名下没有该要求，不显示登记成功，不新增记录、
// 不改写原文件；即使去掉空白能找到其他对象也不能据此登记。普通空格、
// 制表符、全角空格一样不能被忽略。
func TestCLIEnrollWhitespaceNotFoundRejected(t *testing.T) {
	cases := []struct {
		label   string
		args    []string
		wantErr []string
	}{
		// 学生编号带前后空白：文件中有 "s1" 与 " s1 "，这些完整编号都不存在。
		{"学生前导空格", []string{"enroll", " s1", "r1", "2024春", "e1"}, []string{"学生", "不存在"}},
		{"学生尾随空格", []string{"enroll", "s1 ", "r1", "2024春", "e1"}, []string{"学生", "不存在"}},
		{"学生制表符", []string{"enroll", "\ts1\t", "r1", "2024春", "e1"}, []string{"学生", "不存在"}},
		{"学生全角空格", []string{"enroll", "　s1　", "r1", "2024春", "e1"}, []string{"学生", "不存在"}},
		// 学生存在但完整要求编号不在本人名下。
		{"s1名下无带空白要求", []string{"enroll", "s1", " r1 ", "2024春", "e1"}, []string{"s1", "名下不存在要求"}},
		{"要求制表符", []string{"enroll", "s1", "\tr1\t", "2024春", "e1"}, []string{"s1", "名下不存在要求"}},
		{"要求全角空格", []string{"enroll", "s1", "　r1　", "2024春", "e1"}, []string{"s1", "名下不存在要求"}},
		{"带空白学生名下无带空白要求", []string{"enroll", " s1 ", " r1 ", "2024春", "e1"}, []string{" s1 ", "名下不存在要求"}},
		{"完全不存在的要求", []string{"enroll", "s1", "rX", "2024春", "e1"}, []string{"s1", "名下不存在要求"}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			file, raw := writeDiskRecord(t, enrollWhitespaceCLIRecord())
			out, errText, code := runCLI(t, file, tc.args...)
			if code != exitRejected {
				t.Fatalf("%s：应业务拒绝（退出码 1），code=%d out=%q err=%q",
					tc.label, code, out, errText)
			}
			if strings.Contains(out, "已登记修读") {
				t.Fatalf("%s：被拒绝时不应显示登记成功，out=%q", tc.label, out)
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(errText, want) {
					t.Fatalf("%s：错误输出应包含 %q，err=%q", tc.label, want, errText)
				}
			}
			assertFileByteIdentical(t, file, raw, tc.label+"：")

			// 两名已有学生名下都不应出现新修读，学分与要求满足情况保持原样。
			for _, sid := range []string{"s1", " s1 "} {
				show, _, _ := runCLI(t, file, "show", sid)
				if strings.Contains(show, "修读 e1") || strings.Contains(show, "修读  e1") {
					t.Fatalf("%s：不应给 %s 新增修读，show=%q", tc.label, sid, show)
				}
				check, _, _ := runCLI(t, file, "check", sid)
				if !strings.Contains(check, "总学分：0") ||
					!strings.Contains(check, "未满足要求：[r1]") {
					t.Fatalf("%s：%s 的学分与要求应保持原样，check=%q",
						tc.label, sid, check)
				}
			}
		})
	}
}

// TestCLIEnrollWhitespaceEmptyAndBlankIDsRejected 空字符串编号退出码 1；
// 新修读编号只有空白（空格、制表符、全角空格）时仍不得建立；以上拒绝都
// 不新增记录、不改写文件。学期沿用既有输入规则：空或全空白拒绝，带前后
// 空白按修剪后的文字登记。
func TestCLIEnrollWhitespaceEmptyAndBlankIDsRejected(t *testing.T) {
	d := &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
	}

	// 空字符串编号：退出码 1 且文件保持原样。
	for _, args := range [][]string{
		{"enroll", "", "r1", "2024春", "e1"},
		{"enroll", "s1", "", "2024春", "e1"},
		{"enroll", "s1", "r1", "2024春", ""},
	} {
		file, raw := writeDiskRecord(t, d)
		out, errText, code := runCLI(t, file, args...)
		if code != exitRejected || !strings.Contains(errText, "不能为空") {
			t.Fatalf("空编号 %v 应业务拒绝，code=%d out=%q err=%q", args, code, out, errText)
		}
		assertFileByteIdentical(t, file, raw, "空编号拒绝：")
	}

	// 新修读编号只有空白：不得建立。
	for _, enrID := range []string{" ", "\t", "　", " 　 \t"} {
		file, raw := writeDiskRecord(t, d)
		out, errText, code := runCLI(t, file, "enroll", "s1", "r1", "2024春", enrID)
		if code != exitRejected || !strings.Contains(errText, "空白") {
			t.Fatalf("只有空白的修读编号 %q 应拒绝，code=%d out=%q err=%q",
				enrID, code, out, errText)
		}
		if strings.Contains(out, "已登记修读") {
			t.Fatalf("纯空白修读编号被拒时不应显示登记成功，out=%q", out)
		}
		assertFileByteIdentical(t, file, raw, "纯空白修读编号拒绝：")
	}

	// 空学期、纯空白学期仍按既有规则拒绝。
	for _, term := range []string{"", " ", "\t", "　"} {
		file, raw := writeDiskRecord(t, d)
		out, errText, code := runCLI(t, file, "enroll", "s1", "r1", term, "e1")
		if code != exitRejected || !strings.Contains(errText, "学期") {
			t.Fatalf("学期 %q 应拒绝，code=%d out=%q err=%q", term, code, out, errText)
		}
		assertFileByteIdentical(t, file, raw, "空白学期拒绝：")
	}

	// 带前后空白的学期按修剪后的文字登记；再以修剪后的学期重复登记幂等。
	file, _ := writeDiskRecord(t, d)
	out, errText, code := runCLI(t, file, "enroll", "s1", "r1", " 2024春 ", "e1")
	if code != 0 || !strings.Contains(out, "学期 2024春") ||
		!strings.Contains(out, "状态：选课") {
		t.Fatalf("带前后空白的学期应按修剪后文字登记，code=%d out=%q err=%q",
			code, out, errText)
	}
	out, _, code = runCLI(t, file, "enroll", "s1", "r1", "2024春", "e1")
	if code != 0 || !strings.Contains(out, "已存在且内容一致") {
		t.Fatalf("学期按修剪后文字匹配应幂等返回，code=%d out=%q", code, out)
	}
	show, _, _ := runCLI(t, file, "show", "s1")
	if !strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：选课") {
		t.Fatalf("学期原文应按修剪后文字保存，show=%q", show)
	}
}
