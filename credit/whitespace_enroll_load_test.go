package credit

import (
	"strings"
	"testing"
)

// 本文件为“选课登记按用户给出的完整编号确定修读归属”这项行为提供回归
// 保障。记录文件可以合法保存带前后空白的学生编号、要求编号与修读编号，
// Load 按原样接受（见 whitespace_submit_load_test.go 的 mustLoadWhitespace），
// enroll 也必须如此：
//   - “s1”与“ s1 ”是两名不同学生，同一学生名下“r1”与“ r1 ”是两项
//     不同要求、“e1”与“ e1 ”是两份不同修读；普通空格、制表符、全角
//     空格（U+3000）都是编号内容；
//   - 给带空白的完整编号登记修读只落在编号完全一致的学生与要求名下，
//     新修读保留完整编号、初始结果为选课；另一名学生或另一项要求的
//     修读、学分与满足情况保持原样；
//   - 完整编号找不到学生时明确报学生不存在，学生存在但名下没有该完整
//     要求编号时明确报本人名下没有该要求；即使去掉空白能碰上另一条
//     记录，也绝不借用那份记录或把修读登记到别人名下；
//   - 空字符串编号仍拒绝；只由空白字符组成的修读编号不得建立；
//   - 学期沿用现有输入规则：前后空白不计入学期内容，修剪后为空拒绝；
//   - 准确命中后沿用现有规则：相同内容重复登记幂等返回原记录，换要求
//     或换学期拒绝，课程停开后重复登记仍返回原记录、真正新增拒绝。

// TestAddEnrollmentWhitespaceStudentIDExact 学生 "s1" 与 " s1 " 各有要求
// r1 与修读 e1：给 " s1 " 登记新修读 e2 只落在 " s1 " 名下并保留完整
// 编号，"s1" 的修读保持原样。
func TestAddEnrollmentWhitespaceStudentIDExact(t *testing.T) {
	s, _ := mustLoadWhitespace(t, whitespaceBase())

	e, action, err := s.AddEnrollment(" s1 ", "r1", "2024春", "e2")
	if err != nil || action != ActionCreated {
		t.Fatalf("给 \" s1 \" 登记 e2 应成功，action=%v err=%v", action, err)
	}
	if e.StudentID != " s1 " || e.ID != "e2" || e.ReqID != "r1" || e.Result != Enrolled {
		t.Fatalf("新修读应保留完整编号且初始为选课，得到 %+v", e)
	}

	// " s1 " 名下有 e1、e2 两份修读；"s1" 名下仍只有 e1，没有被误登记。
	if got := s.Enrollments(" s1 "); len(got) != 2 {
		t.Fatalf("\" s1 \" 应有 e1、e2 两份修读，得到 %+v", got)
	}
	if got := s.Enrollments("s1"); len(got) != 1 || got[0].ID != "e1" {
		t.Fatalf("\"s1\" 的修读不应变化，得到 %+v", got)
	}
	if s.Enrollment("s1", "e2") != nil {
		t.Fatal("e2 不应登记到 \"s1\" 名下")
	}
}

// TestAddEnrollmentWhitespaceReqIDExact 同一学生名下 "r1" 与 " r1 " 是
// 两项不同要求：给 " r1 " 登记修读必须指向 " r1 " 本身，不能落到去空白
// 后的 "r1" 上。
func TestAddEnrollmentWhitespaceReqIDExact(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "大学物理", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s1"}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: "c1"},
			{ID: " r1 ", StudentID: "s1", CourseID: "c2"},
		},
	}
	s, _ := mustLoadWhitespace(t, d)

	e, action, err := s.AddEnrollment("s1", " r1 ", "2024春", "e1")
	if err != nil || action != ActionCreated {
		t.Fatalf("给 \" r1 \" 登记修读应成功，action=%v err=%v", action, err)
	}
	if e.ReqID != " r1 " {
		t.Fatalf("修读应指向完整要求编号 \" r1 \"，得到 %+v", e)
	}
	if got := s.Requirement("s1", "r1"); got == nil || got.CourseID != "c1" {
		t.Fatalf("要求 \"r1\" 不应被改写，得到 %+v", got)
	}
}

// TestAddEnrollmentWhitespaceEnrollmentIDIndependent 同一学生名下 "e1"
// 与 " e1 " 是两份独立修读，可以在同一学期指向同一要求：登记 " e1 "
// 不能返回 "e1" 的已有记录，也不能借用它的通过结果；新修读初始为选课。
func TestAddEnrollmentWhitespaceEnrollmentIDIndependent(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "大学物理", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s1"}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: "c1"},
			{ID: "r2", StudentID: "s1", CourseID: "c2"},
		},
		Enrollments: []*Enrollment{
			{ID: "e1", StudentID: "s1", ReqID: "r1", Term: "2024春", Result: Passed, ResultSeq: 1},
		},
		NextResultSeq: 1,
	}
	s, _ := mustLoadWhitespace(t, d)

	// 同学期同要求登记 " e1 "：是新建修读，不是返回 "e1"，结果不是通过。
	e, action, err := s.AddEnrollment("s1", "r1", "2024春", " e1 ")
	if err != nil || action != ActionCreated {
		t.Fatalf("登记 \" e1 \" 应作为独立修读被接受，action=%v err=%v", action, err)
	}
	if e.ID != " e1 " || e.Result != Enrolled {
		t.Fatalf("新修读 \" e1 \" 应保留完整编号且初始为选课，得到 %+v", e)
	}
	if got := s.Enrollment("s1", "e1"); got.Result != Passed {
		t.Fatalf("原有 \"e1\" 的通过结果不应被改写，得到 %+v", got)
	}

	// 已有完整修读编号、要求与学期都相同：返回那份原记录，不新增、不重置
	// 成绩。学期输入沿用现有规则，前后空白不计入学期内容。
	again, action, err := s.AddEnrollment("s1", "r1", " 2024春 ", " e1 ")
	if err != nil || action != ActionExisted || again != e {
		t.Fatalf("相同内容重复登记应幂等返回原记录，action=%v err=%v", action, err)
	}
	if again.Result != Enrolled {
		t.Fatalf("重复登记不应重置成绩，得到 %+v", again)
	}

	// 同一完整编号换要求或换学期仍拒绝，原记录保留。
	if _, _, err := s.AddEnrollment("s1", "r2", "2024春", " e1 "); err == nil {
		t.Fatal("相同修读编号换要求应被拒绝")
	}
	if _, _, err := s.AddEnrollment("s1", "r1", "2024秋", " e1 "); err == nil {
		t.Fatal("相同修读编号换学期应被拒绝")
	}
	if got := s.Enrollment("s1", " e1 "); got.ReqID != "r1" || got.Term != "2024春" {
		t.Fatalf("拒绝后 \" e1 \" 应保持原要求与学期，得到 %+v", got)
	}
}

// TestAddEnrollmentWhitespaceNoBorrow 完整编号找不到时必须明确拒绝：
// 即使去掉空白能碰上另一名学生或另一项要求，也绝不借用那份记录，更不
// 把修读登记到别人名下；空字符串编号仍拒绝；只由空白字符组成的新修读
// 编号不得建立。所有拒绝都不产生变更。
func TestAddEnrollmentWhitespaceNoBorrow(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
		},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
	}

	// 学生编号带前后空白：记录里只有 "s1"，应明确报告该学生不存在。
	for _, student := range []string{" s1", "s1 ", " s1 ", "\ts1\t", "　s1　"} {
		s, _ := mustLoadWhitespace(t, d)
		_, _, err := s.AddEnrollment(student, "r1", "2024春", "e1")
		if err == nil || !strings.Contains(err.Error(), "不存在") ||
			!strings.Contains(err.Error(), "学生") {
			t.Fatalf("学生 %q 不存在，登记应明确拒绝，得到 %v", student, err)
		}
		if got := s.Enrollments("s1"); len(got) != 0 {
			t.Fatalf("学生 %q 被拒绝后不应在 \"s1\" 名下新增修读，得到 %+v", student, got)
		}
		if s.Dirty() {
			t.Fatalf("被拒绝的登记不应产生变更（学生 %q）", student)
		}
	}

	// 学生存在但要求编号带前后空白：应明确报告本人名下没有该要求。
	for _, req := range []string{" r1", "r1 ", " r1 ", "\tr1\t", "　r1　"} {
		s, _ := mustLoadWhitespace(t, d)
		_, _, err := s.AddEnrollment("s1", req, "2024春", "e1")
		if err == nil || !strings.Contains(err.Error(), "不存在") ||
			!strings.Contains(err.Error(), "s1") {
			t.Fatalf("要求 %q 不在 s1 名下，登记应明确拒绝，得到 %v", req, err)
		}
		if got := s.Enrollments("s1"); len(got) != 0 {
			t.Fatalf("要求 %q 被拒绝后不应新增修读，得到 %+v", req, got)
		}
		if s.Dirty() {
			t.Fatalf("被拒绝的登记不应产生变更（要求 %q）", req)
		}
	}

	// 空字符串编号仍拒绝。
	s, _ := mustLoadWhitespace(t, d)
	for _, args := range [][4]string{
		{"", "r1", "2024春", "e1"},
		{"s1", "", "2024春", "e1"},
		{"s1", "r1", "2024春", ""},
	} {
		if _, _, err := s.AddEnrollment(args[0], args[1], args[2], args[3]); err == nil {
			t.Fatalf("空字符串编号应被拒绝：%v", args)
		}
	}

	// 只由空白字符组成的新修读编号不得建立：普通空格、制表符、全角空格
	// 一样不行。
	for _, enrID := range []string{" ", "  ", "\t", "　"} {
		if _, _, err := s.AddEnrollment("s1", "r1", "2024春", enrID); err == nil {
			t.Fatalf("只含空白的修读编号 %q 不应建立", enrID)
		}
		if got := s.Enrollment("s1", enrID); got != nil {
			t.Fatalf("不应建立编号 %q 的修读，得到 %+v", enrID, got)
		}
	}
	if got := s.Enrollments("s1"); len(got) != 0 {
		t.Fatalf("全部拒绝后不应有任何修读，得到 %+v", got)
	}
	if s.Dirty() {
		t.Fatal("全部被拒绝的登记不应产生变更")
	}
}

// TestAddEnrollmentWhitespaceClosedCourse 课程停开后：已有相同修读的
// 重复登记继续返回原记录，真正新增修读仍拒绝——带完整空白编号也一样。
func TestAddEnrollmentWhitespaceClosedCourse(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: false},
		},
		Students:     []*Student{{ID: " s1 "}},
		Requirements: []*Requirement{{ID: "r1", StudentID: " s1 ", CourseID: "c1"}},
		Enrollments: []*Enrollment{
			{ID: " e1 ", StudentID: " s1 ", ReqID: "r1", Term: "2024春", Result: Enrolled},
		},
	}
	s, _ := mustLoadWhitespace(t, d)

	// 相同（学生、要求、学期、完整修读编号）重复登记：幂等返回原记录。
	e, action, err := s.AddEnrollment(" s1 ", "r1", "2024春", " e1 ")
	if err != nil || action != ActionExisted {
		t.Fatalf("停开后重复登记已有修读应返回原记录，action=%v err=%v", action, err)
	}
	if e.ID != " e1 " || e.StudentID != " s1 " {
		t.Fatalf("返回的应是完整编号 \" e1 \" 的原记录，得到 %+v", e)
	}

	// 真正新增修读仍拒绝。
	if _, _, err := s.AddEnrollment(" s1 ", "r1", "2024春", "e2"); err == nil {
		t.Fatal("课程停开后不应允许新增修读")
	}
	if got := s.Enrollments(" s1 "); len(got) != 1 {
		t.Fatalf("拒绝新增后 \" s1 \" 应仍只有一份修读，得到 %+v", got)
	}
}

// TestAddEnrollmentWhitespaceTermRules 学期沿用现有输入规则：前后空白
// 不计入学期内容，修剪后为空则拒绝；完整编号的修读记录本身不受影响。
func TestAddEnrollmentWhitespaceTermRules(t *testing.T) {
	s, _ := mustLoadWhitespace(t, whitespaceBase())

	// 学期只含空白：拒绝，不建立修读。
	if _, _, err := s.AddEnrollment(" s1 ", "r1", "   ", "e2"); err == nil {
		t.Fatal("只含空白的学期应被拒绝")
	}
	if got := s.Enrollment(" s1 ", "e2"); got != nil {
		t.Fatalf("被拒绝后不应建立修读，得到 %+v", got)
	}

	// 学期带前后空白：按现有规则修剪后正常登记，保存修剪后的学期。
	e, action, err := s.AddEnrollment(" s1 ", "r1", " 2024秋 ", "e2")
	if err != nil || action != ActionCreated {
		t.Fatalf("学期带前后空白应按现有规则正常登记，action=%v err=%v", action, err)
	}
	if e.Term != "2024秋" || e.StudentID != " s1 " {
		t.Fatalf("学期应修剪而编号保留完整文字，得到 %+v", e)
	}
}
