package main

import (
	"strings"
	"testing"
)

// 本文件从命令行入口回归“pass/fail 按用户给出的完整学生编号与修读编号
// 确定成绩归属”。记录文件可以合法保存带前后空白（普通空格、制表符、
// 全角空格）的编号，查询能把它们区分，提交结果也必须如此：
//   - 给 " s1 " 提交通过只命中本人的 e1，核对按本人要求获得学分并以
//     本人修读说明来源；"s1" 的状态、学分与来源保持原样；
//   - 同一学生名下 "e1" 与 " e1 " 分别处理，提交一份不影响另一份，
//     不误报重复提交或结果冲突；
//   - 完整编号找不到时按业务拒绝（退出码 1），明确报告学生不存在或
//     该学生名下不存在该修读，不输出提交成功的提示，不借用去空白后
//     能碰上的记录，也不补建修读，记录文件逐字节保持原样；
//   - 含前后空白的编号是合法记录，不能判为文件损坏（退出码 2）。

// whitespaceCLIRecord 构造合法记录：学生 "s1" 与 " s1 " 各自的要求 r1
// 指向 4 学分课程 c1，名下各有一份尚未提交结果的修读 e1。
func whitespaceCLIRecord() *diskRecord {
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
		Enrollments: []diskEnr{
			{ID: "e1", Student: "s1", Req: "r1", Term: "2024春", Result: "enrolled"},
			{ID: "e1", Student: " s1 ", Req: "r1", Term: "2024春", Result: "enrolled"},
		},
	}
}

// TestCLIPassWhitespaceStudentIDHitsExactOwner 给 " s1 " 提交通过：
// 只有这名学生的 e1 变为通过，核对按其本人要求获得 4 学分并以本人的
// 修读说明来源；"s1" 的修读状态、学分和来源保持原样。
func TestCLIPassWhitespaceStudentIDHitsExactOwner(t *testing.T) {
	file, _ := writeDiskRecord(t, whitespaceCLIRecord())

	out, errText, code := runCLI(t, file, "pass", " s1 ", "e1")
	if code != 0 || !strings.Contains(out, "通过") {
		t.Fatalf("给 \" s1 \" 的 e1 提交通过应成功，code=%d out=%q err=%q", code, out, errText)
	}

	// " s1 " 本人：4 学分、来源为本人修读 e1。
	out, _, code = runCLI(t, file, "check", " s1 ")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("\" s1 \" 应凭本人 e1 获得 4 学分，code=%d out=%q", code, out)
	}
	// "s1" 保持原样：仍是选课、0 学分、要求未满足。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("\"s1\" 应仍为 0 学分且 r1 未满足，code=%d out=%q", code, out)
	}
	if out, _, _ := runCLI(t, file, "show", "s1"); !strings.Contains(out, "结果：选课") {
		t.Fatalf("\"s1\" 的 e1 不应被改写，out=%q", out)
	}
}

// TestCLIPassWhitespaceEnrollmentIDIndependent 同一学生名下 "e1" 与
// " e1 " 是两份独立修读：给 " e1 " 提交未通过只记录该份未通过，不得
// 学分，原有要求与学期不变；"e1" 的通过结果与学分保持原样，两次独立
// 修读不被误认为重复提交或结果冲突。
func TestCLIPassWhitespaceEnrollmentIDIndependent(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students: []diskStudent{
			{ID: "s1"},
		},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
		},
		Enrollments: []diskEnr{
			{ID: "e1", Student: "s1", Req: "r1", Term: "2024春", Result: "passed", ResultSeq: 1},
			{ID: " e1 ", Student: "s1", Req: "r1", Term: "2024秋", Result: "enrolled"},
		},
		NextResultSeq: 1,
	}
	file, _ := writeDiskRecord(t, d)

	out, errText, code := runCLI(t, file, "fail", "s1", " e1 ")
	if code != 0 || !strings.Contains(out, "未通过") {
		t.Fatalf("给 \" e1 \" 提交未通过应作为独立修读被接受，code=%d out=%q err=%q",
			code, out, errText)
	}

	// " e1 " 未通过不得学分；学分与来源仍来自 "e1" 的通过。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("学分与来源应保持来自 \"e1\" 的通过，code=%d out=%q", code, out)
	}
	// 两份修读各自保留：学期与结果原样。
	show, _, _ := runCLI(t, file, "show", "s1")
	if !strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：通过") ||
		!strings.Contains(show, "修读  e1 ：要求 r1，学期 2024秋，结果：未通过") {
		t.Fatalf("两份修读应各自保留学期与结果，out=%q", show)
	}
}

// TestCLIPassFailWhitespaceIDRejected 完整编号找不到时按业务拒绝：
// 退出码 1，明确报告学生不存在或该学生名下不存在该修读，不输出提交
// 成功的提示；即使去掉空白能碰上另一条记录也不借用、不补建；普通空格、
// 制表符、全角空格一样不能被忽略；记录文件逐字节保持原样。
func TestCLIPassFailWhitespaceIDRejected(t *testing.T) {
	d := &diskRecord{
		Version:  1,
		Courses:  []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students: []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
		},
		Enrollments: []diskEnr{
			{ID: "e1", Student: "s1", Req: "r1", Term: "2024春", Result: "enrolled"},
		},
	}

	cases := []struct {
		label   string
		args    []string
		wantErr []string
	}{
		// 学生编号带前后空白：文件中只有 "s1"，应明确报告学生不存在。
		{"学生前导空格", []string{"pass", " s1", "e1"}, []string{"学生", "不存在"}},
		{"学生尾随空格", []string{"pass", "s1 ", "e1"}, []string{"学生", "不存在"}},
		{"学生制表符", []string{"fail", "\ts1\t", "e1"}, []string{"学生", "不存在"}},
		{"学生全角空格", []string{"pass", "　s1　", "e1"}, []string{"学生", "不存在"}},
		// 学生存在但修读编号带前后空白：应明确报告该学生名下不存在该修读。
		{"修读前导空格", []string{"pass", "s1", " e1"}, []string{"s1", "不存在"}},
		{"修读尾随空格", []string{"fail", "s1", "e1 "}, []string{"s1", "不存在"}},
		{"修读制表符", []string{"pass", "s1", "\te1\t"}, []string{"s1", "不存在"}},
		{"修读全角空格", []string{"fail", "s1", "　e1　"}, []string{"s1", "不存在"}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			file, raw := writeDiskRecord(t, d)
			out, errText, code := runCLI(t, file, tc.args...)
			if code != exitRejected {
				t.Fatalf("%s：应业务拒绝（退出码 1），code=%d out=%q err=%q",
					tc.label, code, out, errText)
			}
			if strings.Contains(out, "已提交") || strings.Contains(out, "结果已提交") {
				t.Fatalf("%s：被拒绝时不应输出提交成功的提示，out=%q", tc.label, out)
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(errText, want) {
					t.Fatalf("%s：错误输出应包含 %q，err=%q", tc.label, want, errText)
				}
			}
			assertFileByteIdentical(t, file, raw, tc.label+"：")
		})
	}
}

// TestCLIPassWhitespaceIdempotentNoRewrite 准确命中带空白编号的修读后，
// 重复提交相同结果幂等返回原记录且不再写文件；已有结果改提另一结果仍
// 拒绝并保留原记录。
func TestCLIPassWhitespaceIdempotentNoRewrite(t *testing.T) {
	file, _ := writeDiskRecord(t, whitespaceCLIRecord())

	if _, errText, code := runCLI(t, file, "pass", " s1 ", "e1"); code != 0 {
		t.Fatalf("首次提交通过应成功，code=%d err=%q", code, errText)
	}
	before := mustReadRecord(t, file)

	// 重复提交相同结果：返回原记录，文件不再改写。
	out, _, code := runCLI(t, file, "pass", " s1 ", "e1")
	if code != 0 || !strings.Contains(out, "已提交过相同结果") {
		t.Fatalf("重复提交相同结果应幂等返回，code=%d out=%q", code, out)
	}
	assertRecordUnchanged(t, file, before, "幂等重复提交：")

	// 改提另一结果：退出码 1，原结果与文件保持原样。
	out, errText, code := runCLI(t, file, "fail", " s1 ", "e1")
	if code != exitRejected || strings.Contains(out, "结果已提交") {
		t.Fatalf("改提另一结果应拒绝，code=%d out=%q err=%q", code, out, errText)
	}
	assertRecordUnchanged(t, file, before, "改提另一结果：")
	if out, _, _ := runCLI(t, file, "show", " s1 "); !strings.Contains(out, "结果：通过") {
		t.Fatalf("拒绝后 \" s1 \" 的 e1 应保留通过，out=%q", out)
	}
}
