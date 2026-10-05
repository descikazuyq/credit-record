package credit

import (
	"strings"
	"testing"
)

// 本文件为“登记课程要求时按用户给出的完整编号确定学生与课程”这项行为
// 提供回归保障。记录文件可以合法保存带前后空白的学生编号、课程编号与
// 要求编号，Load 按原样接受（见 whitespace_submit_load_test.go 的
// mustLoadWhitespace），req 也必须与查询、选课、免修使用同一组对象：
//   - “s1”与“ s1 ”是两名不同学生，“c1”与“ c1 ”是两门不同课程，
//     同一学生名下“r1”与“ r1 ”是两项不同要求；普通空格、制表符、
//     全角空格（U+3000）都是编号内容；
//   - 给带空白的完整编号登记要求只落在编号完全一致的学生名下、指向
//     编号完全一致的课程，三个编号原文都保留；另一名学生的要求、
//     修读与学分保持原样；
//   - 完整学生编号或完整课程编号找不到时明确报对应对象不存在；即使
//     去掉空白能碰上另一条记录，也绝不借用那份记录，更不自动创建
//     学生或课程；
//   - 空字符串编号仍拒绝；要求编号只由空白字符组成时不得建立；
//   - 准确命中后沿用现有规则：同一学生名下不同空白写法的同号要求必须
//     指向不同课程；同一学生不能换编号为同一课程再建要求；完整三元组
//     相同幂等返回原记录；同一完整要求编号改指另一课程整次拒绝；
//   - 课程停开不改变建立要求的规则：停开课程仍可登记要求，新要求本身
//     不带来学分。

// reqWhitespaceData 构造一份“同号带空白”对象并存的合法记录：
// 学生 "s1" 与 " s1 "、课程 "c1" 与 " c1 "（名称、学分各不相同），
// "s1" 名下有要求 r1 指向 "c1"，" s1 " 名下有要求 r1 指向 " c1 "。
func reqWhitespaceData() *fileData {
	return &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: " c1 ", Name: "全角物理", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s1"}, {ID: " s1 "}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: "c1"},
			{ID: "r1", StudentID: " s1 ", CourseID: " c1 "},
		},
	}
}

// TestAddRequirementWhitespaceStudentIDExact 为 " s1 " 登记新要求时，
// 只在 " s1 " 名下保存这一份要求并保留完整学生编号；"s1" 的要求保持
// 原样。课程用 "s1" 的 r1 已引用的 "c1"：重复课程限制按学生隔离，
// " s1 " 仍可就该课程建立要求。
func TestAddRequirementWhitespaceStudentIDExact(t *testing.T) {
	s, _ := mustLoadWhitespace(t, reqWhitespaceData())

	r, action, err := s.AddRequirement(" s1 ", "r2", "c1")
	if err != nil || action != ActionCreated {
		t.Fatalf("给 \" s1 \" 登记 r2 应成功，action=%v err=%v", action, err)
	}
	if r.StudentID != " s1 " || r.ID != "r2" || r.CourseID != "c1" {
		t.Fatalf("新要求应登记在完整编号学生名下，得到 %+v", r)
	}

	// " s1 " 名下有 r1、r2 两份要求；"s1" 名下仍只有 r1，没有被误登记。
	if got := s.Requirements(" s1 "); len(got) != 2 {
		t.Fatalf("\" s1 \" 应有两份要求，得到 %+v", got)
	}
	if got := s.Requirements("s1"); len(got) != 1 || got[0].ID != "r1" {
		t.Fatalf("\"s1\" 的要求不应变化，得到 %+v", got)
	}
	if s.Requirement("s1", "r2") != nil {
		t.Fatal("r2 不应登记到 \"s1\" 名下")
	}
}

// TestAddRequirementWhitespaceAllIDsPreserved 题目主场景：记录中同时有
// "s1" 与 " s1 " 两名学生、"c1" 与 " c1 " 两门课程，为 " s1 " 登记
// 指向 " c1 " 的要求 " r1 "，只在该学生名下保存这一份要求，三个编号
// 原文全部保留；另一名学生的要求与学分保持原样。
func TestAddRequirementWhitespaceAllIDsPreserved(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: " c1 ", Name: "大学物理", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s1"}, {ID: " s1 "}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: "c1"},
		},
	}
	s, _ := mustLoadWhitespace(t, d)

	r, action, err := s.AddRequirement(" s1 ", " r1 ", " c1 ")
	if err != nil || action != ActionCreated {
		t.Fatalf("为 \" s1 \" 登记 \" r1 \" -> \" c1 \" 应成功，action=%v err=%v", action, err)
	}
	if r.StudentID != " s1 " || r.ID != " r1 " || r.CourseID != " c1 " {
		t.Fatalf("三个编号原文都应保留，得到 %+v", r)
	}

	// 只在 " s1 " 名下新增这一份要求。
	got := s.Requirement(" s1 ", " r1 ")
	if got == nil || got.CourseID != " c1 " {
		t.Fatalf("\" s1 \" 名下应能按完整编号查到新要求，得到 %+v", got)
	}
	if s.Requirement("s1", " r1 ") != nil {
		t.Fatal("带空白的要求编号不应出现在 \"s1\" 名下")
	}
	if s.Requirement(" s1 ", "r1") != nil {
		t.Fatal("不应把去空白后的 \"r1\" 也登记到 \" s1 \" 名下")
	}
	if all := s.AllRequirements(); len(all) != 2 {
		t.Fatalf("整份记录应仍只有两份要求，得到 %+v", all)
	}

	// 另一名学生的要求与课程指向保持原样。
	orig := s.Requirement("s1", "r1")
	if orig == nil || orig.CourseID != "c1" {
		t.Fatalf("\"s1\" 的 r1 应仍指向 \"c1\"，得到 %+v", orig)
	}
}

// TestAddRequirementWhitespaceReqIDDistinctButCourseGuard 同一学生名下
// "r1" 与 " r1 " 是两项不同要求，可以并存，但仍须指向不同课程；指向
// 同一门课程时按“同学生同课程不得重复建立”拒绝。
func TestAddRequirementWhitespaceReqIDDistinctButCourseGuard(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "大学物理", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s1"}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: "c1"},
		},
	}
	s, _ := mustLoadWhitespace(t, d)

	// " r1 " 是另一项要求，指向不同课程 c2：允许新建。
	r, action, err := s.AddRequirement("s1", " r1 ", "c2")
	if err != nil || action != ActionCreated {
		t.Fatalf("同号不同空白的要求指向不同课程应允许，action=%v err=%v", action, err)
	}
	if r.ID != " r1 " || r.CourseID != "c2" {
		t.Fatalf("新要求应保留完整编号并指向 c2，得到 %+v", r)
	}
	if got := s.Requirement("s1", "r1"); got == nil || got.CourseID != "c1" {
		t.Fatalf("原要求 r1 -> c1 不应变化，得到 %+v", got)
	}

	// 再用第三个写法的编号指向 c1：同一学生就同一课程换编号仍拒绝。
	if _, _, err := s.AddRequirement("s1", "r2", "c1"); err == nil ||
		!strings.Contains(err.Error(), "重复建立") {
		t.Fatalf("同一学生就同一课程换编号再建应拒绝，得到 %v", err)
	}
	// " r1 " 也不能再指向 c1（c1 已被 r1 引用）。
	if _, _, err := s.AddRequirement("s1", " r1 ", "c1"); err == nil {
		t.Fatal("同一完整要求编号改指另一门课程应拒绝")
	}
	if got := s.Requirement("s1", " r1 "); got.CourseID != "c2" {
		t.Fatalf("拒绝改指后 \" r1 \" 应保留原指向 c2，得到 %+v", got)
	}
	if got := s.Requirements("s1"); len(got) != 2 {
		t.Fatalf("拒绝后仍应只有 r1、 r1 两份要求，得到 %+v", got)
	}
}

// TestAddRequirementWhitespaceIdempotentExact 完整学生编号、要求编号与
// 课程编号均与已有要求相同时返回原记录，不新增要求、不标记变更。
func TestAddRequirementWhitespaceIdempotentExact(t *testing.T) {
	s, _ := mustLoadWhitespace(t, reqWhitespaceData())
	s.dirty = false

	existing := s.Requirement(" s1 ", "r1")
	got, action, err := s.AddRequirement(" s1 ", "r1", " c1 ")
	if err != nil || action != ActionExisted || got != existing {
		t.Fatalf("完整三元组相同应幂等返回原记录，action=%v err=%v got=%+v", action, err, got)
	}
	if s.Dirty() {
		t.Fatal("幂等返回不应标记变更")
	}

	// 仅课程编号的空白不同就是另一门课程：不得当成幂等命中，而应按
	// “同号要求改指课程”拒绝（"c1" 这门课存在）。
	if _, _, err := s.AddRequirement(" s1 ", "r1", "c1"); err == nil ||
		!strings.Contains(err.Error(), "不能改指") {
		t.Fatalf("完整编号不同的课程应视为改指并拒绝，得到 %v", err)
	}
	if c := s.Requirement(" s1 ", "r1").CourseID; c != " c1 " {
		t.Fatalf("原指向 \" c1 \" 应保留，得到 %q", c)
	}
}

// TestAddRequirementWhitespaceNoBorrow 完整编号找不到时必须明确拒绝：
// 即使去掉空白能碰上另一名学生或另一门课程，也绝不借用、不自动创建；
// 空字符串编号仍拒绝；要求编号全部由空白字符组成时拒绝。所有拒绝都
// 不产生变更。
func TestAddRequirementWhitespaceNoBorrow(t *testing.T) {
	// 学生不存在场景：记录里只有 "s1"。
	dStudent := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
		},
		Students:     []*Student{{ID: "s1"}},
		Requirements: nil,
	}
	for _, student := range []string{" s1", "s1 ", " s1 ", "\ts1\t", "　s1　"} {
		s, _ := mustLoadWhitespace(t, dStudent)
		_, _, err := s.AddRequirement(student, "r1", "c1")
		if err == nil || !strings.Contains(err.Error(), "不存在") ||
			!strings.Contains(err.Error(), "学生") {
			t.Fatalf("学生 %q 不存在，登记应明确拒绝，得到 %v", student, err)
		}
		if got := s.Requirement(student, "r1"); got != nil {
			t.Fatalf("不应自动创建学生 %q 并登记要求", student)
		}
		if s.Dirty() {
			t.Fatalf("被拒绝的登记不应产生变更（学生 %q）", student)
		}
	}

	// 课程不存在场景：记录里只有 "c1"；学生用 "s1" 与 " s1 " 两名，
	// 验证带空白的课程编号不会借用去空白后的课程。
	dCourse := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
		},
		Students: []*Student{{ID: "s1"}, {ID: " s1 "}},
	}
	for _, course := range []string{" c1", "c1 ", " c1 ", "\tc1\t", "　c1　"} {
		s, _ := mustLoadWhitespace(t, dCourse)
		_, _, err := s.AddRequirement(" s1 ", "r1", course)
		if err == nil || !strings.Contains(err.Error(), "不存在") ||
			!strings.Contains(err.Error(), "课程") {
			t.Fatalf("课程 %q 不存在，登记应明确拒绝，得到 %v", course, err)
		}
		if got := s.Requirements(" s1 "); len(got) != 0 {
			t.Fatalf("课程 %q 被拒绝后不应新增要求，得到 %+v", course, got)
		}
		if s.Course(course) != nil {
			t.Fatalf("不应自动创建课程 %q", course)
		}
		if s.Dirty() {
			t.Fatalf("被拒绝的登记不应产生变更（课程 %q）", course)
		}
	}

	// 空字符串编号仍拒绝。
	s, _ := mustLoadWhitespace(t, dCourse)
	for _, args := range [3][3]string{
		{"", "r1", "c1"},
		{"s1", "", "c1"},
		{"s1", "r1", ""},
	} {
		if _, _, err := s.AddRequirement(args[0], args[1], args[2]); err == nil {
			t.Fatalf("空字符串编号应被拒绝：%v", args)
		}
	}

	// 要求编号全部由空白字符组成时拒绝：普通空格、制表符、全角空格一样。
	for _, reqID := range []string{" ", "  ", "\t", "　"} {
		if _, _, err := s.AddRequirement("s1", reqID, "c1"); err == nil ||
			!strings.Contains(err.Error(), "要求编号") {
			t.Fatalf("只含空白的要求编号 %q 应被明确拒绝，得到 %v", reqID, err)
		}
		if got := s.Requirement("s1", reqID); got != nil {
			t.Fatalf("不应建立编号 %q 的要求，得到 %+v", reqID, got)
		}
	}
	if got := s.AllRequirements(); len(got) != 0 {
		t.Fatalf("全部拒绝后不应有任何要求，得到 %+v", got)
	}
	if s.Dirty() {
		t.Fatal("全部被拒绝的登记不应产生变更")
	}
}

// TestAddRequirementWhitespaceClosedCourseAllowed 课程是否停开不改变
// 建立要求的既有规则：停开课程仍可登记新要求，新要求初始未满足、不
// 带来学分，之后只有修读通过或有效免修才能满足。
func TestAddRequirementWhitespaceClosedCourseAllowed(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: " c1 ", Name: "大学物理", Credit: 3, Open: false},
		},
		Students: []*Student{{ID: " s1 "}},
	}
	s, _ := mustLoadWhitespace(t, d)

	r, action, err := s.AddRequirement(" s1 ", " r1 ", " c1 ")
	if err != nil || action != ActionCreated {
		t.Fatalf("停开课程仍应允许登记要求，action=%v err=%v", action, err)
	}
	if r.CourseID != " c1 " || r.ID != " r1 " {
		t.Fatalf("应保留完整编号，得到 %+v", r)
	}
	// 新要求本身不带来学分、未满足。
	rep := s.CheckStudent(" s1 ")
	if !rep.Found || rep.TotalCredits != 0 || len(rep.Unmet) != 1 || rep.Unmet[0] != " r1 " {
		t.Fatalf("新要求应未满足且不计学分，得到 %+v", rep)
	}
}
