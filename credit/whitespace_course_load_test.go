package credit

import (
	"strings"
	"testing"
)

// 本文件为“course 登记按用户给出的完整编号区分课程”这项行为提供回归保障。
// 课程编号与要求、修读、免修、查询使用同一口径：前后空白（普通空格、
// 制表符、全角空格 U+3000）都是编号内容，绝不修剪：
//   - “c1”与“ c1 ”是两门各自独立的课程，分别登记时各自保留完整编号、
//     名称与正整数学分，初始开放，课程列表分别显示；
//   - 记录里只有“c1”时，提交“ c1 ”必须新建后者，不能借用前者的名称、
//     学分或停开状态；已有文件中的带空白编号课程同样按原编号维护；
//   - 再次提交某个完整编号只判断对应课程：名称与学分相同幂等返回原记录，
//     内容不同按现有规则原地更新，不新增副本、不改动另一门课程；
//   - 学分修改限制跟随实际命中的课程：任何学生的要求引用了“ c1 ”，该
//     课程就不能改学分（连同新名称整次拒绝，原名称、学分、开放状态全部
//     保留）；仅“c1”被引用不能阻止未被引用的“ c1 ”更新，反之亦然；
//   - 被引用课程保持原学分时仍允许改名，已有要求、修读、免修继续关联
//     原课程，核对的满足情况、学分来源与总学分不因此变化；
//   - 重新登记停开课程后仍须停开；
//   - 空字符串与全部由空白字符组成的课程编号仍拒绝；含实际文字的编号
//     原样保存并参与重复判断。

// courseWhitespaceData 构造一份“同号带空白”课程并存的合法记录：
// 课程 "c1"（高等数学 4 学分，开放）与 " c1 "（大学物理 3 学分，开放），
// 学生 "s1" 名下要求 r1 指向 " c1 "。
func courseWhitespaceData() *fileData {
	return &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: " c1 ", Name: "大学物理", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s1"}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: " c1 "},
		},
	}
}

// TestAddCourseWhitespaceDistinctCourses 首次分别登记 "c1" 与 " c1 "：
// 保存两门独立课程，各自保留完整编号、名称与学分，初始开放。
func TestAddCourseWhitespaceDistinctCourses(t *testing.T) {
	s := NewStore()
	c1, a, err := s.AddCourse("c1", "高等数学", 4)
	if err != nil || a != ActionCreated {
		t.Fatalf("登记 \"c1\" 应成功，action=%v err=%v", a, err)
	}
	c2, a, err := s.AddCourse(" c1 ", "大学物理", 3)
	if err != nil || a != ActionCreated {
		t.Fatalf("登记 \" c1 \" 应作为新课程成功，action=%v err=%v", a, err)
	}
	if c1 == c2 {
		t.Fatal("\"c1\" 与 \" c1 \" 应是两门独立课程")
	}
	if c2.ID != " c1 " || c2.Name != "大学物理" || c2.Credit != 3 || !c2.Open {
		t.Fatalf("\" c1 \" 应保留完整编号、名称、学分并初始开放，得到 %+v", c2)
	}
	if c1.Name != "高等数学" || c1.Credit != 4 || !c1.Open {
		t.Fatalf("\"c1\" 不应受另一门登记影响，得到 %+v", c1)
	}
	if got := s.Courses(); len(got) != 2 {
		t.Fatalf("课程列表应有两门课程，得到 %+v", got)
	}
	if s.Course(" c1 ") != c2 || s.Course("c1") != c1 {
		t.Fatal("两门课程都应能按各自的完整编号查到")
	}
}

// TestAddCourseWhitespaceNoBorrowFromTrimmed 记录里只有 "c1"（停开）时，
// 提交 " c1 " 必须新建后者：初始开放，不借用 "c1" 的名称、学分或停开
// 状态；"c1" 本身保持原样。
func TestAddCourseWhitespaceNoBorrowFromTrimmed(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: false},
		},
	}
	s, _ := mustLoadWhitespace(t, d)

	c, a, err := s.AddCourse(" c1 ", "大学物理", 3)
	if err != nil || a != ActionCreated {
		t.Fatalf("文件中只有 \"c1\" 时 \" c1 \" 应新建，action=%v err=%v", a, err)
	}
	if c.ID != " c1 " || c.Name != "大学物理" || c.Credit != 3 {
		t.Fatalf("新课程应保留本次提交的完整编号、名称与学分，得到 %+v", c)
	}
	if !c.Open {
		t.Fatal("新建课程应初始开放，不能借用 \"c1\" 的停开状态")
	}
	orig := s.Course("c1")
	if orig.Name != "高等数学" || orig.Credit != 4 || orig.Open {
		t.Fatalf("\"c1\" 的资料与停开状态应保持原样，得到 %+v", orig)
	}
	if got := s.Courses(); len(got) != 2 {
		t.Fatalf("应保存两门独立课程，得到 %+v", got)
	}
}

// TestAddCourseWhitespaceIdempotentAndUpdate 再次提交某个完整编号时只判断
// 对应课程：名称与学分相同幂等返回原记录、不标记变更；内容不同原地更新，
// 不新增副本、不改动另一门课程。
func TestAddCourseWhitespaceIdempotentAndUpdate(t *testing.T) {
	s, _ := mustLoadWhitespace(t, courseWhitespaceData())
	s.dirty = false

	// 完整编号、名称、学分都相同：幂等返回原记录。
	existing := s.Course(" c1 ")
	got, a, err := s.AddCourse(" c1 ", "大学物理", 3)
	if err != nil || a != ActionExisted || got != existing {
		t.Fatalf("完整编号同内容应幂等返回原记录，action=%v err=%v got=%+v", a, err, got)
	}
	if s.Dirty() {
		t.Fatal("幂等返回不应标记变更")
	}

	// 内容不同（" c1 " 虽被 r1 引用，但学分不变只改名）：原地更新，
	// 不新增副本，另一门课程 "c1" 不受影响。
	got, a, err = s.AddCourse(" c1 ", "大学物理（上）", 3)
	if err != nil || a != ActionUpdated || got != existing {
		t.Fatalf("同学分改名应原地更新，action=%v err=%v got=%+v", a, err, got)
	}
	if got.ID != " c1 " || got.Name != "大学物理（上）" || got.Credit != 3 {
		t.Fatalf("更新应保留完整编号并应用新名称，得到 %+v", got)
	}
	if all := s.Courses(); len(all) != 2 {
		t.Fatalf("更新不应新增副本，仍应只有两门课程，得到 %+v", all)
	}
	if c1 := s.Course("c1"); c1.Name != "高等数学" || c1.Credit != 4 {
		t.Fatalf("另一门课程 \"c1\" 不应被改动，得到 %+v", c1)
	}
}

// TestAddCourseWhitespaceCreditLockFollowsHitCourse 学分修改限制跟随实际
// 命中的课程：要求引用的是 " c1 "，则 " c1 " 不能改学分（连同新名称整次
// 拒绝，原名称、学分、开放状态全部保留）；未被引用的 "c1" 仍可更新。
// 反过来只引用 "c1" 时，" c1 " 仍可更新。
func TestAddCourseWhitespaceCreditLockFollowsHitCourse(t *testing.T) {
	s, _ := mustLoadWhitespace(t, courseWhitespaceData())

	// " c1 " 被 r1 引用：新名称 + 不同学分整次拒绝。
	_, _, err := s.AddCourse(" c1 ", "大学物理（下）", 5)
	if err == nil || !strings.Contains(err.Error(), "已被课程要求引用") {
		t.Fatalf("被引用的 \" c1 \" 改学分应拒绝，得到 %v", err)
	}
	c := s.Course(" c1 ")
	if c.Name != "大学物理" || c.Credit != 3 || !c.Open {
		t.Fatalf("整次拒绝后原名称、学分、开放状态应全部保留，得到 %+v", c)
	}

	// 未被引用的 "c1" 不受该限制：可以更新名称与学分。
	if _, a, err := s.AddCourse("c1", "高等数学（上）", 6); err != nil || a != ActionUpdated {
		t.Fatalf("未被引用的 \"c1\" 应允许更新，action=%v err=%v", a, err)
	}

	// 反过来：只有 "c1" 被引用时，" c1 " 仍可更新，"c1" 被锁定。
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: " c1 ", Name: "大学物理", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s1"}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: "c1"},
		},
	}
	s2, _ := mustLoadWhitespace(t, d)
	if _, a, err := s2.AddCourse(" c1 ", "大学物理（下）", 5); err != nil || a != ActionUpdated {
		t.Fatalf("未被引用的 \" c1 \" 应允许更新，action=%v err=%v", a, err)
	}
	if _, _, err := s2.AddCourse("c1", "高等数学（下）", 6); err == nil {
		t.Fatal("被引用的 \"c1\" 改学分应拒绝")
	}
	if c := s2.Course("c1"); c.Name != "高等数学" || c.Credit != 4 {
		t.Fatalf("\"c1\" 整次拒绝后应保持原样，得到 %+v", c)
	}
}

// TestAddCourseWhitespaceRenameReferencedKeepsLinks 被引用课程保持原学分
// 时仍允许改名：已有要求、修读继续关联原课程，核对的满足情况、学分来源
// 与总学分不因此变化。
func TestAddCourseWhitespaceRenameReferencedKeepsLinks(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: " c1 ", Name: "大学物理", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s1"}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: " c1 "},
		},
		Enrollments: []*Enrollment{
			{ID: "e1", StudentID: "s1", ReqID: "r1", Term: "2024春", Result: Passed, ResultSeq: 1},
		},
		NextResultSeq: 1,
	}
	s, _ := mustLoadWhitespace(t, d)

	if _, a, err := s.AddCourse(" c1 ", "大学物理（强化班）", 3); err != nil || a != ActionUpdated {
		t.Fatalf("被引用课程保持原学分改名应允许，action=%v err=%v", a, err)
	}

	// 要求与修读仍关联原课程编号 " c1 "。
	r := s.Requirement("s1", "r1")
	if r == nil || r.CourseID != " c1 " {
		t.Fatalf("要求应继续指向完整编号 \" c1 \"，得到 %+v", r)
	}
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 3 || len(rep.Unmet) != 0 {
		t.Fatalf("改名后满足情况与总学分不应变化，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	st := rep.Requirements[0]
	if !st.Satisfied || st.Source != "enrollment" || st.PassedEnrollmentID != "e1" {
		t.Fatalf("改名后学分来源应仍为通过修读 e1，得到 %+v", st)
	}
}

// TestAddCourseWhitespaceClosedStaysClosed 重新登记停开的带空白编号课程：
// 允许按现有规则更新，但停开状态必须保持，不能借重新登记恢复开放。
func TestAddCourseWhitespaceClosedStaysClosed(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: " c1 ", Name: "大学物理", Credit: 3, Open: false},
		},
	}
	s, _ := mustLoadWhitespace(t, d)

	c, a, err := s.AddCourse(" c1 ", "大学物理（下）", 5)
	if err != nil || a != ActionUpdated {
		t.Fatalf("停开课程未被引用时应允许更新，action=%v err=%v", a, err)
	}
	if c.Open {
		t.Fatal("重新登记停开课程后仍须停开")
	}
	// 同内容重复提交也保持停开。
	c, a, err = s.AddCourse(" c1 ", "大学物理（下）", 5)
	if err != nil || a != ActionExisted || c.Open {
		t.Fatalf("幂等重复后仍应停开，action=%v open=%v err=%v", a, c.Open, err)
	}
}

// TestAddCourseWhitespaceIDRejected 空字符串与全部由空白字符组成的课程
// 编号仍拒绝（普通空格、制表符、全角空格一样）；含实际文字的编号原样
// 保存并参与重复判断。所有拒绝都不产生变更。
func TestAddCourseWhitespaceIDRejected(t *testing.T) {
	s := NewStore()
	if _, _, err := s.AddCourse("", "高等数学", 4); err == nil {
		t.Fatal("空字符串课程编号应被拒绝")
	}
	for _, id := range []string{" ", "  ", "\t", "　", " \t　"} {
		if _, _, err := s.AddCourse(id, "高等数学", 4); err == nil {
			t.Fatalf("只含空白的课程编号 %q 应被拒绝", id)
		}
		if s.Course(id) != nil {
			t.Fatalf("不应建立编号 %q 的课程", id)
		}
	}
	if got := s.Courses(); len(got) != 0 {
		t.Fatalf("全部拒绝后不应有任何课程，得到 %+v", got)
	}
	if s.Dirty() {
		t.Fatal("全部被拒绝的登记不应产生变更")
	}

	// 含实际文字的编号原样保存，并参与重复判断：同完整编号同内容幂等，
	// 空白不同就是另一门课程。
	if _, a, err := s.AddCourse("\tc1\t", "高等数学", 4); err != nil || a != ActionCreated {
		t.Fatalf("含实际文字的编号 %q 应原样保存，action=%v err=%v", "\tc1\t", a, err)
	}
	if c := s.Course("\tc1\t"); c == nil || c.ID != "\tc1\t" {
		t.Fatalf("编号 %q 应按原文保存并可查询，得到 %+v", "\tc1\t", c)
	}
	if _, a, err := s.AddCourse("\tc1\t", "高等数学", 4); err != nil || a != ActionExisted {
		t.Fatalf("同完整编号同内容应幂等，action=%v err=%v", a, err)
	}
	if _, a, err := s.AddCourse("c1", "大学物理", 3); err != nil || a != ActionCreated {
		t.Fatalf("空白不同的 \"c1\" 应是另一门课程，action=%v err=%v", a, err)
	}
	if got := s.Courses(); len(got) != 2 {
		t.Fatalf("应保存两门独立课程，得到 %+v", got)
	}
}
