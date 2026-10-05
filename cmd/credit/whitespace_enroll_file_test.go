package main

import (
	"strings"
	"testing"
)

// 本文件从命令行入口回归“enroll 按用户给出的完整学生编号、要求编号与
// 修读编号确定修读归属”。记录文件可以合法保存带前后空白（普通空格、
// 制表符、全角空格）的编号，查询与提交结果能把它们区分，选课登记也
// 必须如此：
//   - 给 " s1 " 登记修读只落在这名学生名下，成功提示与 show 的修读
//     归属一致；"s1" 的修读、学分与要求满足情况保持原样；
//   - 同一学生名下 "e1" 与 " e1 " 是两份独立修读，可在同一学期指向
//     同一要求；登记一份不返回另一份的已有记录，也不借用它的通过结果；
//   - 完整编号找不到时按业务拒绝（退出码 1），明确报告学生不存在或
//     本人名下没有该要求，不输出登记成功的提示，不借用去空白后能碰上
//     的记录，也不新增修读，记录文件逐字节保持原样；
//   - 空字符串编号仍拒绝，只含空白的新修读编号不得建立；
//   - 课程停开后，已有相同修读的重复登记继续返回原记录，真正新增仍拒绝。

// TestCLIEnrollWhitespaceStudentIDHitsExactOwner 给 " s1 " 登记新修读
// e2：成功提示与 show 都把 e2 归在 " s1 " 名下；"s1" 的修读、学分与
// 要求满足情况保持原样。
func TestCLIEnrollWhitespaceStudentIDHitsExactOwner(t *testing.T) {
	file, _ := writeDiskRecord(t, whitespaceCLIRecord())

	out, errText, code := runCLI(t, file, "enroll", " s1 ", "r1", "2024春", "e2")
	if code != 0 || !strings.Contains(out, "已登记修读 e2") ||
		!strings.Contains(out, "学生  s1 ") || !strings.Contains(out, "状态：选课") {
		t.Fatalf("给 \" s1 \" 登记 e2 应成功并归在本人名下，code=%d out=%q err=%q",
			code, out, errText)
	}

	// show 中的归属与成功提示一致：e2 出现在 " s1 " 名下，初始为选课。
	out, _, code = runCLI(t, file, "show", " s1 ")
	if code != 0 || !strings.Contains(out, "修读 e2：要求 r1，学期 2024春，结果：选课") {
		t.Fatalf("\" s1 \" 名下应列出 e2（选课），code=%d out=%q", code, out)
	}
	// "s1" 保持原样：只有 e1（选课）、0 学分、要求未满足。
	out, _, _ = runCLI(t, file, "show", "s1")
	if strings.Contains(out, "e2") {
		t.Fatalf("e2 不应出现在 \"s1\" 名下，out=%q", out)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("\"s1\" 应仍为 0 学分且 r1 未满足，code=%d out=%q", code, out)
	}
}

// TestCLIEnrollWhitespaceEnrollmentIDIndependent 同一学生名下 "e1" 与
// " e1 " 是两份独立修读：同一学期同一要求登记 " e1 " 是新建修读（选课），
// 不返回 "e1" 的已有记录，也不借用它的通过结果；相同内容重复登记返回
// 原记录，换学期拒绝。
func TestCLIEnrollWhitespaceEnrollmentIDIndependent(t *testing.T) {
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
		},
		NextResultSeq: 1,
	}
	file, _ := writeDiskRecord(t, d)

	out, errText, code := runCLI(t, file, "enroll", "s1", "r1", "2024春", " e1 ")
	if code != 0 || !strings.Contains(out, "已登记修读  e1 ") ||
		!strings.Contains(out, "状态：选课") {
		t.Fatalf("登记 \" e1 \" 应作为独立修读被接受，code=%d out=%q err=%q",
			code, out, errText)
	}

	// 两份修读各自保留："e1" 仍是通过，" e1 " 是选课；学分与来源不变。
	show, _, _ := runCLI(t, file, "show", "s1")
	if !strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：通过") ||
		!strings.Contains(show, "修读  e1 ：要求 r1，学期 2024春，结果：选课") {
		t.Fatalf("两份修读应各自保留结果，out=%q", show)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("学分与来源应保持来自 \"e1\" 的通过，code=%d out=%q", code, out)
	}

	// 相同内容重复登记：返回原记录，不新增、不重置成绩，文件不再改写。
	before := mustReadRecord(t, file)
	out, _, code = runCLI(t, file, "enroll", "s1", "r1", "2024春", " e1 ")
	if code != 0 || !strings.Contains(out, "已存在且内容一致，返回原记录") {
		t.Fatalf("重复登记 \" e1 \" 应幂等返回原记录，code=%d out=%q", code, out)
	}
	assertRecordUnchanged(t, file, before, "幂等重复登记：")

	// 同一完整编号换学期仍拒绝，原记录保留。
	out, errText, code = runCLI(t, file, "enroll", "s1", "r1", "2024秋", " e1 ")
	if code != exitRejected || strings.Contains(out, "已登记修读") {
		t.Fatalf("相同修读编号换学期应拒绝，code=%d out=%q err=%q", code, out, errText)
	}
	assertRecordUnchanged(t, file, before, "换学期拒绝：")
}

// TestCLIEnrollWhitespaceIDRejected 完整编号找不到时按业务拒绝：退出码 1，
// 明确报告学生不存在或本人名下没有该要求，不输出登记成功的提示；即使去掉
// 空白能碰上另一条记录也不借用、不登记；普通空格、制表符、全角空格一样
// 不能被忽略；空字符串编号与只含空白的新修读编号同样拒绝；记录文件逐
// 字节保持原样。
func TestCLIEnrollWhitespaceIDRejected(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students: []diskStudent{
			{ID: "s1"},
		},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
		},
	}

	cases := []struct {
		label   string
		args    []string
		wantErr []string
	}{
		// 学生编号带前后空白：文件中只有 "s1"，应明确报告学生不存在。
		{"学生前导空格", []string{"enroll", " s1", "r1", "2024春", "e1"}, []string{"学生", "不存在"}},
		{"学生尾随空格", []string{"enroll", "s1 ", "r1", "2024春", "e1"}, []string{"学生", "不存在"}},
		{"学生制表符", []string{"enroll", "\ts1\t", "r1", "2024春", "e1"}, []string{"学生", "不存在"}},
		{"学生全角空格", []string{"enroll", "　s1　", "r1", "2024春", "e1"}, []string{"学生", "不存在"}},
		// 学生存在但要求编号带前后空白：应明确报告本人名下没有该要求。
		{"要求前导空格", []string{"enroll", "s1", " r1", "2024春", "e1"}, []string{"s1", "不存在"}},
		{"要求尾随空格", []string{"enroll", "s1", "r1 ", "2024春", "e1"}, []string{"s1", "不存在"}},
		{"要求制表符", []string{"enroll", "s1", "\tr1\t", "2024春", "e1"}, []string{"s1", "不存在"}},
		{"要求全角空格", []string{"enroll", "s1", "　r1　", "2024春", "e1"}, []string{"s1", "不存在"}},
		// 空字符串编号仍拒绝。
		{"学生空字符串", []string{"enroll", "", "r1", "2024春", "e1"}, []string{"不能为空"}},
		{"要求空字符串", []string{"enroll", "s1", "", "2024春", "e1"}, []string{"不能为空"}},
		{"修读空字符串", []string{"enroll", "s1", "r1", "2024春", ""}, []string{"不能为空"}},
		// 只含空白的新修读编号不得建立。
		{"修读编号全空格", []string{"enroll", "s1", "r1", "2024春", "  "}, []string{"修读编号"}},
		{"修读编号全角空格", []string{"enroll", "s1", "r1", "2024春", "　"}, []string{"修读编号"}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			file, raw := writeDiskRecord(t, d)
			out, errText, code := runCLI(t, file, tc.args...)
			if code != exitRejected {
				t.Fatalf("%s：应业务拒绝（退出码 1），code=%d out=%q err=%q",
					tc.label, code, out, errText)
			}
			if strings.Contains(out, "已登记修读") {
				t.Fatalf("%s：被拒绝时不应输出登记成功的提示，out=%q", tc.label, out)
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

// TestCLIEnrollWhitespaceClosedCourse 课程停开后：已有相同修读（完整空白
// 编号）的重复登记继续返回原记录，真正新增修读仍拒绝。
func TestCLIEnrollWhitespaceClosedCourse(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: false}},
		Students: []diskStudent{
			{ID: " s1 "},
		},
		Requirements: []diskReq{
			{ID: "r1", Student: " s1 ", Course: "c1"},
		},
		Enrollments: []diskEnr{
			{ID: " e1 ", Student: " s1 ", Req: "r1", Term: "2024春", Result: "enrolled"},
		},
	}
	file, _ := writeDiskRecord(t, d)

	// 相同内容重复登记：返回原记录。
	out, errText, code := runCLI(t, file, "enroll", " s1 ", "r1", "2024春", " e1 ")
	if code != 0 || !strings.Contains(out, "已存在且内容一致，返回原记录") {
		t.Fatalf("停开后重复登记已有修读应返回原记录，code=%d out=%q err=%q",
			code, out, errText)
	}

	// 真正新增修读仍拒绝。
	before := mustReadRecord(t, file)
	out, errText, code = runCLI(t, file, "enroll", " s1 ", "r1", "2024春", "e2")
	if code != exitRejected || !strings.Contains(errText, "停开") ||
		strings.Contains(out, "已登记修读") {
		t.Fatalf("停开后新增修读应拒绝，code=%d out=%q err=%q", code, out, errText)
	}
	assertRecordUnchanged(t, file, before, "停开后新增：")
}
