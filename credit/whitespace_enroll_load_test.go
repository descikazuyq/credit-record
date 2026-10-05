package credit

import (
	"strings"
	"testing"
)

// 本文件为“选课登记时按用户给出的完整学生编号、要求编号与修读编号确定
// 修读归属”这项行为提供回归保障，与成绩提交、免修申请/撤销对完整编号的
// 处理保持一致。记录文件可以合法保存带前后空白的编号（登记学生/要求入口
// 会修剪编号，但既有文件中的编号原文不得改写，Load 按原样接受），因此
// 用例直接构造记录文件，聚焦 enroll 的选对象逻辑：
//   - “s1”与“ s1 ”是两名不同学生，同一学生名下“r1”与“ r1 ”是两项
//     不同要求，“e1”与“ e1 ”是两份不同修读；普通空格、制表符、全角
//     空格（U+3000）都是编号内容，绝不能为了匹配而删掉；
//   - 给带空白的编号登记修读只命中编号完全一致的学生与要求，新修读保留
//     完整编号、初始结果为选课，另一名学生的修读、学分与要求满足情况
//     保持原样；
//   - 同一完整修读编号、要求与学期重复登记返回原记录（幂等，不重置结果）；
//     同一完整编号改用其他要求或学期仍拒绝；“e1”与“ e1 ”互不借用；
//   - 课程停开后，已有修读的重复登记仍返回原记录，真正新增修读仍拒绝；
//   - 完整学生编号不存在时明确报学生不存在，学生存在但完整要求编号不在
//     本人名下时明确报本人名下没有该要求：即使去掉空白能碰上其他对象，
//     也绝不据此登记，不新增记录、不产生变更；
//   - 空字符串编号仍拒绝，新修读编号只有空白时仍不得建立；学期继续沿用
//     既有输入规则（只去首尾空白）。

// enrollTwoStudents 构造两名学生 "s1" 与 " s1 "：各有一项编号为 r1、
// 指向同一门 4 学分开放课程 c1 的要求，两人均无修读。
func enrollTwoStudents() *fileData {
	return &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
		},
		Students: []*Student{{ID: "s1"}, {ID: " s1 "}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: "c1"},
			{ID: "r1", StudentID: " s1 ", CourseID: "c1"},
		},
	}
}

// TestEnrollWhitespaceStudentIDHitsExactOwner 给 " s1 " 选课：修读只挂在
// " s1 " 本人名下并保留完整编号，初始为选课；"s1" 名下不出现任何修读，
// 其学分与要求满足情况保持原样。随后给新修读提交通过，学分也只计入本人。
func TestEnrollWhitespaceStudentIDHitsExactOwner(t *testing.T) {
	s, _ := mustLoadWhitespace(t, enrollTwoStudents())

	e, action, err := s.AddEnrollment(" s1 ", "r1", "2024春", "e1")
	if err != nil || action != ActionCreated {
		t.Fatalf("给 \" s1 \" 选课应正常新建，得到 %+v action=%v err=%v", e, action, err)
	}
	if e.StudentID != " s1 " || e.ReqID != "r1" || e.ID != "e1" ||
		e.Term != "2024春" || e.Result != Enrolled {
		t.Fatalf("新修读应保留完整编号且初始为选课，得到 %+v", e)
	}
	if got := s.Enrollment(" s1 ", "e1"); got != e {
		t.Fatalf("新修读应能按完整编号在 \" s1 \" 名下查到，得到 %+v", got)
	}
	if es := s.Enrollments(" s1 "); len(es) != 1 || es[0] != e {
		t.Fatalf("\" s1 \" 名下应只有这一份新修读，得到 %+v", es)
	}

	// "s1" 名下不应出现任何修读。
	if es := s.Enrollments("s1"); len(es) != 0 {
		t.Fatalf("修读不应记到 \"s1\" 名下，得到 %+v", es)
	}

	// 给新修读提交通过：只影响 " s1 " 本人。
	if _, changed, err := s.SubmitResult(" s1 ", "e1", Passed); err != nil || !changed {
		t.Fatalf("\" s1 \" 的 e1 提交通过应成功，changed=%v err=%v", changed, err)
	}
	if rep := s.CheckStudent(" s1 "); !rep.Found || rep.TotalCredits != 4 ||
		len(rep.Unmet) != 0 || rep.Requirements[0].PassedEnrollmentID != "e1" {
		t.Fatalf("\" s1 \" 应凭本人 e1 获得 4 学分，得到 %+v", rep)
	}
	if rep := s.CheckStudent("s1"); !rep.Found || rep.TotalCredits != 0 ||
		len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("\"s1\" 应仍为 0 学分且 r1 未满足，得到 %+v", rep)
	}
}

// TestEnrollWhitespaceReqIDIndependent 同一学生名下 "r1"（4 学分）与
// " r1 "（3 学分）是两项不同要求：给 " r1 " 选课只挂到完整编号对应的
// 要求上；同一学期可以再给 r1 选课，两份修读各自独立，互不顶替。
func TestEnrollWhitespaceReqIDIndependent(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "线性代数", Credit: 3, Open: true},
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
		t.Fatalf("给完整编号 \" r1 \" 选课应新建，得到 %+v action=%v err=%v", e, action, err)
	}
	if e.ReqID != " r1 " || e.Result != Enrolled {
		t.Fatalf("新修读应保留完整要求编号 \" r1 \" 且初始为选课，得到 %+v", e)
	}

	// 同一学期再给不带空白的 r1 选课：不同要求、不同编号，允许并存。
	e2, action, err := s.AddEnrollment("s1", "r1", "2024春", "e2")
	if err != nil || action != ActionCreated || e2.ReqID != "r1" {
		t.Fatalf("给 r1 的同学期选课应作为独立修读新建，得到 %+v action=%v err=%v",
			e2, action, err)
	}

	// e1 通过后只满足 " r1 "（3 学分），r1 仍未满足。
	if _, changed, err := s.SubmitResult("s1", "e1", Passed); err != nil || !changed {
		t.Fatalf("e1 提交通过应成功，changed=%v err=%v", changed, err)
	}
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 3 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("只应计 \" r1 \" 的 3 学分且 r1 未满足，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	statuses := map[string]RequirementStatus{}
	for _, st := range rep.Requirements {
		statuses[st.Req.ID] = st
	}
	if st := statuses[" r1 "]; !st.Satisfied || st.PassedEnrollmentID != "e1" ||
		st.Course.Credit != 3 {
		t.Fatalf("\" r1 \" 应由本人 e1 满足并计 3 学分，得到 %+v", st)
	}
	if st := statuses["r1"]; st.Satisfied {
		t.Fatalf("r1 不应被同号异写要求的修读满足，得到 %+v", st)
	}
}

// TestEnrollWhitespaceEnrollmentIDIndependent 同一学生名下 "e1" 与 " e1 "
// 是两份独立修读：登记 " e1 " 不能返回已通过的 "e1"，也不能借用它的
// 通过结果；新修读初始为选课，与原修读在同一学期指向同一要求也允许并存。
func TestEnrollWhitespaceEnrollmentIDIndependent(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
		},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
		Enrollments: []*Enrollment{
			{ID: "e1", StudentID: "s1", ReqID: "r1", Term: "2024春",
				Result: Passed, ResultSeq: 1},
		},
		NextResultSeq: 1,
	}
	s, _ := mustLoadWhitespace(t, d)

	e, action, err := s.AddEnrollment("s1", "r1", "2024春", " e1 ")
	if err != nil || action != ActionCreated {
		t.Fatalf("\" e1 \" 应作为独立修读新建，而不是返回已有 \"e1\"，"+
			"得到 %+v action=%v err=%v", e, action, err)
	}
	if e.ID != " e1 " || e.Result != Enrolled {
		t.Fatalf("新修读应保留完整编号 \" e1 \" 且初始为选课，得到 %+v", e)
	}
	if got := s.Enrollment("s1", "e1"); got == nil || got.Result != Passed {
		t.Fatalf("原有 \"e1\" 的通过结果不应被改动或借用，得到 %+v", got)
	}
	if es := s.Enrollments("s1"); len(es) != 2 {
		t.Fatalf("两份修读应并存，得到 %+v", es)
	}
	// 学分仍只来自 "e1" 的通过；新修读是选课，不计学分。
	if rep := s.CheckStudent("s1"); rep.TotalCredits != 4 ||
		rep.Requirements[0].PassedEnrollmentID != "e1" {
		t.Fatalf("学分来源应仍是原通过修读 \"e1\"，得到 %+v", rep)
	}

	// 反向也成立：名下只有 " e1 " 时，登记 "e1" 是新建而非幂等返回。
	d2 := &fileData{
		Version:      recordVersion,
		Courses:      []*Course{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
		Enrollments: []*Enrollment{
			{ID: " e1 ", StudentID: "s1", ReqID: "r1", Term: "2024秋", Result: Enrolled},
		},
	}
	s2, _ := mustLoadWhitespace(t, d2)
	e2, action, err := s2.AddEnrollment("s1", "r1", "2024秋", "e1")
	if err != nil || action != ActionCreated || e2.Result != Enrolled {
		t.Fatalf("名下只有 \" e1 \" 时登记 \"e1\" 应新建，得到 %+v action=%v err=%v",
			e2, action, err)
	}
	if got := s2.Enrollment("s1", " e1 "); got == nil || got == e2 {
		t.Fatalf("原 \" e1 \" 应继续独立存在且不被新修读顶替，得到 %+v", got)
	}
}

// TestEnrollWhitespaceIdempotentAndConflictRules 完整编号完全一致时沿用
// 既有规则：相同（学生、修读编号、要求、学期）重复登记返回原记录且不
// 产生变更、不重置结果；同一完整编号改用其他要求或学期仍拒绝并保留原记录。
func TestEnrollWhitespaceIdempotentAndConflictRules(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: " s1 "}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: " s1 ", CourseID: "c1"},
			{ID: "r2", StudentID: " s1 ", CourseID: "c2"},
		},
		Enrollments: []*Enrollment{
			{ID: "e1", StudentID: " s1 ", ReqID: "r1", Term: "2024春",
				Result: Passed, ResultSeq: 1},
		},
		NextResultSeq: 1,
	}
	s, _ := mustLoadWhitespace(t, d)

	// 完全一致的重复登记：返回原记录（含其通过结果），不新增、不重置。
	got, action, err := s.AddEnrollment(" s1 ", "r1", "2024春", "e1")
	if err != nil || action != ActionExisted {
		t.Fatalf("完整编号一致的重复登记应幂等返回原记录，得到 %+v action=%v err=%v",
			got, action, err)
	}
	if got.Result != Passed || got.ResultSeq != 1 {
		t.Fatalf("幂等返回不得重置既有通过结果，得到 %+v", got)
	}
	if s.Dirty() {
		t.Fatal("幂等重复登记不应产生变更")
	}
	if es := s.Enrollments(" s1 "); len(es) != 1 {
		t.Fatalf("幂等重复登记不应新增修读，得到 %+v", es)
	}

	// 同一完整编号换要求：拒绝。
	if _, _, err := s.AddEnrollment(" s1 ", "r2", "2024春", "e1"); err == nil {
		t.Fatal("同一完整修读编号改用其他要求应被拒绝")
	}
	// 同一完整编号换学期：拒绝。
	if _, _, err := s.AddEnrollment(" s1 ", "r1", "2024秋", "e1"); err == nil {
		t.Fatal("同一完整修读编号改用其他学期应被拒绝")
	}
	if e := s.Enrollment(" s1 ", "e1"); e.ReqID != "r1" || e.Term != "2024春" ||
		e.Result != Passed {
		t.Fatalf("冲突拒绝后原记录的要求、学期与结果都应保留，得到 %+v", e)
	}
	if s.Dirty() {
		t.Fatal("冲突拒绝不应产生变更")
	}

	// 带空白的修读编号同理：原内容重复只返回带空白的那一份，不与 "e1" 混。
	d2 := &fileData{
		Version:      recordVersion,
		Courses:      []*Course{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
		Enrollments: []*Enrollment{
			{ID: " e1 ", StudentID: "s1", ReqID: "r1", Term: "2024秋", Result: Enrolled},
		},
	}
	s2, _ := mustLoadWhitespace(t, d2)
	if e, a, err := s2.AddEnrollment("s1", "r1", "2024秋", " e1 "); err != nil ||
		a != ActionExisted || e.ID != " e1 " {
		t.Fatalf("\" e1 \" 原内容重复登记应幂等命中本人，得到 %+v action=%v err=%v",
			e, a, err)
	}
	if _, _, err := s2.AddEnrollment("s1", "r1", "2025春", " e1 "); err == nil {
		t.Fatal("\" e1 \" 换学期登记应被拒绝")
	}
}

// TestEnrollWhitespaceClosedCourse 课程停开后：已有相同修读的重复登记仍
// 返回原记录（即使编号带空白），真正新增修读仍拒绝，且不能借去空白碰上
// 另一名学生或另一份修读绕开停开限制。
func TestEnrollWhitespaceClosedCourse(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: false},
		},
		Students: []*Student{{ID: "s1"}, {ID: " s1 "}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: "c1"},
			{ID: "r1", StudentID: " s1 ", CourseID: "c1"},
		},
		Enrollments: []*Enrollment{
			{ID: "e1", StudentID: "s1", ReqID: "r1", Term: "2024春", Result: Enrolled},
			{ID: "e1", StudentID: " s1 ", ReqID: "r1", Term: "2024春", Result: Enrolled},
		},
	}
	s, _ := mustLoadWhitespace(t, d)

	// 已有相同修读的重复登记：停开课程下仍幂等返回原记录。
	e, action, err := s.AddEnrollment(" s1 ", "r1", "2024春", "e1")
	if err != nil || action != ActionExisted || e.StudentID != " s1 " {
		t.Fatalf("停开后已有修读重复登记应返回 \" s1 \" 的原记录，得到 %+v action=%v err=%v",
			e, action, err)
	}
	if s.Dirty() {
		t.Fatal("已有修读的幂等重复不应产生变更")
	}

	// 真正新增修读：停开课程下拒绝，不论编号是否带空白、用哪名学生。
	for _, tc := range [][4]string{
		{" s1 ", "r1", "2024春", " e1 "},
		{" s1 ", "r1", "2024秋", "e2"},
		{"s1", "r1", "2024秋", "e2"},
	} {
		if _, _, err := s.AddEnrollment(tc[0], tc[1], tc[2], tc[3]); err == nil ||
			!strings.Contains(err.Error(), "停开") {
			t.Fatalf("停开课程下新增修读 %v 应明确拒绝，得到 %v", tc, err)
		}
	}
	// 带尾随空白的学生编号找不到完整对象：报学生不存在，而不是借用 "s1"。
	if _, _, err := s.AddEnrollment("s1 ", "r1", "2024秋", "e9"); err == nil ||
		!strings.Contains(err.Error(), "学生") || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("完整学生编号不存在应优先报学生不存在，得到 %v", err)
	}
	for _, sid := range []string{"s1", " s1 "} {
		if es := s.Enrollments(sid); len(es) != 1 || es[0].ID != "e1" {
			t.Fatalf("拒绝后 %s 名下应仍只有原 e1，得到 %+v", sid, es)
		}
	}
	if s.Dirty() {
		t.Fatal("停开课程下的新增拒绝不应产生变更")
	}
}

// TestEnrollWhitespaceNoBorrow 完整编号找不到对象时必须明确拒绝：即使
// 去掉空白后能碰上另一名学生或另一项要求，也绝不借用那份记录登记修读。
// 普通空格、制表符、全角空格一样不能被忽略；拒绝不新增记录、不产生变更。
func TestEnrollWhitespaceNoBorrow(t *testing.T) {
	// 完整学生编号不存在：文件中只有 "s1"。
	base := &fileData{
		Version:      recordVersion,
		Courses:      []*Course{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
	}
	for _, student := range []string{" s1", "s1 ", " s1 ", "\ts1\t", "　s1　"} {
		s, _ := mustLoadWhitespace(t, base)
		_, _, err := s.AddEnrollment(student, "r1", "2024春", "e1")
		if err == nil {
			t.Fatalf("学生 %q 不存在，选课应明确拒绝", student)
		}
		msg := err.Error()
		if !strings.Contains(msg, "学生") || !strings.Contains(msg, "不存在") {
			t.Fatalf("学生 %q 找不到时应明确报告学生不存在，得到 %v", student, err)
		}
		if es := s.Enrollments("s1"); len(es) != 0 {
			t.Fatalf("学生 %q 被拒后不应在 \"s1\" 名下新增修读，得到 %+v", student, es)
		}
		if es := s.Enrollments(student); len(es) != 0 {
			t.Fatalf("不应为学生 %q 补建修读，得到 %+v", student, es)
		}
		if s.Dirty() {
			t.Fatalf("学生不存在的拒绝不应产生变更（学生 %q）", student)
		}
	}

	// 学生存在但完整要求编号不在本人名下：即使去掉空白能碰上本人或另一名
	// 学生的另一项要求，也明确报本人名下没有该要求，绝不借用。
	for _, tc := range []struct{ student, req string }{
		{"s1", " r1 "},
		{"s1", "\tr1\t"},
		{"s1", "　r1　"},
		{" s1 ", " r1 "},
		{" s1 ", "r2"},
	} {
		s, _ := mustLoadWhitespace(t, enrollTwoStudents())
		_, _, err := s.AddEnrollment(tc.student, tc.req, "2024春", "e9")
		if err == nil {
			t.Fatalf("%s 名下没有要求 %q，选课应明确拒绝", tc.student, tc.req)
		}
		msg := err.Error()
		if !strings.Contains(msg, "名下不存在要求") {
			t.Fatalf("应明确报告本人名下没有该要求，得到 %v", err)
		}
		if !strings.Contains(msg, tc.student) || !strings.Contains(msg, tc.req) {
			t.Fatalf("错误应点名学生 %q 与要求 %q，得到 %v", tc.student, tc.req, err)
		}
		if es := s.Enrollments(tc.student); len(es) != 0 {
			t.Fatalf("%s 被拒后不应新增修读，得到 %+v", tc.student, es)
		}
		if s.Enrollment(tc.student, "e9") != nil {
			t.Fatalf("不应补建修读 e9（学生 %q）", tc.student)
		}
		if s.Dirty() {
			t.Fatalf("要求不存在的拒绝不应产生变更（%s/%s）", tc.student, tc.req)
		}
		other := "s1"
		if tc.student == "s1" {
			other = " s1 "
		}
		if es := s.Enrollments(other); len(es) != 0 {
			t.Fatalf("拒绝不应在另一名学生 %s 名下留下修读，得到 %+v", other, es)
		}
	}
}

// TestEnrollWhitespaceEmptyAndBlankIDsRejected 空字符串编号仍拒绝；纯空白
// 字符串是合法的完整编号而不是空字符串：文件里没有该学生/要求时按“不
// 存在”拒绝；新修读编号只有空白时一律不得建立。学期沿用既有输入规则：
// 只去首尾空白，去完为空拒绝，带空白的学期按修剪后的文字登记与匹配。
func TestEnrollWhitespaceEmptyAndBlankIDsRejected(t *testing.T) {
	s, _ := mustLoadWhitespace(t, &fileData{
		Version:      recordVersion,
		Courses:      []*Course{{ID: "c1", Name: "数学", Credit: 4, Open: true}},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
	})

	// 空字符串编号：直接拒绝。
	for _, tc := range [][4]string{
		{"", "r1", "2024春", "e1"},
		{"s1", "", "2024春", "e1"},
		{"s1", "r1", "2024春", ""},
	} {
		if _, _, err := s.AddEnrollment(tc[0], tc[1], tc[2], tc[3]); err == nil ||
			!strings.Contains(err.Error(), "不能为空") {
			t.Fatalf("空编号 %v 应直接报错，得到 %v", tc, err)
		}
	}
	if es := s.Enrollments("s1"); len(es) != 0 {
		t.Fatalf("空编号拒绝不得创建修读，得到 %+v", es)
	}
	if s.Dirty() {
		t.Fatal("空编号拒绝不应产生变更")
	}

	// 新修读编号只有空白（空格、制表符、全角空格混用）：不得建立。
	for _, enrID := range []string{" ", "\t", "　", " 　 \t"} {
		if _, _, err := s.AddEnrollment("s1", "r1", "2024春", enrID); err == nil ||
			!strings.Contains(err.Error(), "空白") {
			t.Fatalf("只有空白的修读编号 %q 应被拒绝，得到 %v", enrID, err)
		}
	}
	if es := s.Enrollments("s1"); len(es) != 0 {
		t.Fatalf("纯空白修读编号不得建立记录，得到 %+v", es)
	}
	if s.Dirty() {
		t.Fatal("纯空白修读编号的拒绝不应产生变更")
	}

	// 纯空白学生/要求编号不等于空字符串：按完整编号查找，找不到时报不存在。
	if _, _, err := s.AddEnrollment(" ", "r1", "2024春", "e1"); err == nil ||
		!strings.Contains(err.Error(), "学生") || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("纯空白学生编号应按完整编号查找并报学生不存在，得到 %v", err)
	}
	if _, _, err := s.AddEnrollment("s1", " r1 ", "2024春", "e1"); err == nil ||
		!strings.Contains(err.Error(), "名下不存在要求") {
		t.Fatalf("纯空白要求编号应按完整编号查找并报名下不存在，得到 %v", err)
	}
	if s.Dirty() {
		t.Fatal("纯空白编号找不到对象的拒绝不应产生变更")
	}

	// 学期沿用既有输入规则：空串或全空白拒绝；带前后空白按修剪后文字登记。
	if _, _, err := s.AddEnrollment("s1", "r1", "", "e1"); err == nil ||
		!strings.Contains(err.Error(), "学期") {
		t.Fatalf("空学期应被拒绝，得到 %v", err)
	}
	if _, _, err := s.AddEnrollment("s1", "r1", " \t ", "e1"); err == nil {
		t.Fatalf("只有空白的学期应被拒绝，得到 %v", err)
	}
	e, action, err := s.AddEnrollment("s1", "r1", " 2024春 ", "e1")
	if err != nil || action != ActionCreated || e.Term != "2024春" {
		t.Fatalf("带前后空白的学期应按修剪后文字登记，得到 %+v action=%v err=%v",
			e, action, err)
	}
	// 修剪后与既有记录同学期：再次登记按幂等返回，不新增。
	if _, action, err := s.AddEnrollment("s1", "r1", "2024春", "e1"); err != nil ||
		action != ActionExisted {
		t.Fatalf("学期按修剪后文字匹配应幂等返回，得到 action=%v err=%v", action, err)
	}
}
