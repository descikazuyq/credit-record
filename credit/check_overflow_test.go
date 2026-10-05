package credit

import (
	"math"
	"testing"
)

// 满足一项要求：登记修读并提交通过。
func satisfyByPass(t *testing.T, s *Store, student, req, enr string) {
	t.Helper()
	if _, _, err := s.AddEnrollment(student, req, "2024春", enr); err != nil {
		t.Fatalf("登记修读失败：%v", err)
	}
	if _, _, err := s.SubmitResult(student, enr, Passed); err != nil {
		t.Fatalf("提交通过失败：%v", err)
	}
}

// 总学分恰好等于 int 上限时不算超限。
func TestCheckTotalAtMaxIntNoOverflow(t *testing.T) {
	s := NewStore()
	if _, _, err := s.AddStudent("s1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddCourse("c1", "数学", math.MaxInt); err != nil {
		t.Fatalf("上限学分的课程本身合法，应能登记：%v", err)
	}
	if _, _, err := s.AddRequirement("s1", "r1", "c1"); err != nil {
		t.Fatal(err)
	}
	satisfyByPass(t, s, "s1", "r1", "e1")

	rep := s.CheckStudent("s1")
	if rep.Overflow {
		t.Fatalf("总学分恰好等于上限不应判超限")
	}
	if rep.TotalCredits != math.MaxInt {
		t.Fatalf("总学分应为 %d，得到 %d", math.MaxInt, rep.TotalCredits)
	}
}

// 两项各自合法的课程学分相加超出 int 上限时必须标记超限。
func TestCheckTotalOverflow(t *testing.T) {
	s := NewStore()
	if _, _, err := s.AddStudent("s1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddCourse("c1", "数学", math.MaxInt); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddCourse("c2", "物理", 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddRequirement("s1", "r1", "c1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddRequirement("s1", "r2", "c2"); err != nil {
		t.Fatal(err)
	}
	satisfyByPass(t, s, "s1", "r1", "e1")

	// 只有第一项满足：总学分恰好等于上限，不超限。
	rep := s.CheckStudent("s1")
	if rep.Overflow || rep.TotalCredits != math.MaxInt {
		t.Fatalf("只有第一项满足时不应超限，overflow=%v total=%d", rep.Overflow, rep.TotalCredits)
	}

	// 第二项只有选课记录：不计学分，不触发超限。
	if _, _, err := s.AddEnrollment("s1", "r2", "2024春", "e2"); err != nil {
		t.Fatal(err)
	}
	if rep := s.CheckStudent("s1"); rep.Overflow {
		t.Fatalf("第二项仅选课不应触发超限")
	}
	// 第二项未通过：同样不计学分。
	if _, _, err := s.SubmitResult("s1", "e2", Failed); err != nil {
		t.Fatal(err)
	}
	if rep := s.CheckStudent("s1"); rep.Overflow {
		t.Fatalf("第二项未通过不应触发超限")
	}

	// 第二项也通过：总和超出上限，必须标记超限。
	satisfyByPass(t, s, "s1", "r2", "e3")
	rep = s.CheckStudent("s1")
	if !rep.Overflow {
		t.Fatalf("总学分超出上限应标记超限，total=%d", rep.TotalCredits)
	}
	if rep.TotalCredits < 0 {
		t.Fatalf("超限后总学分不得回绕成负数，得到 %d", rep.TotalCredits)
	}
}

// 超限判断必须基于实际获得的学分：同一要求多次通过、或通过修读与有效
// 免修并存，仍只计一份课程学分。
func TestCheckOverflowCountsEachRequirementOnce(t *testing.T) {
	s := NewStore()
	if _, _, err := s.AddStudent("s1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddCourse("c1", "数学", math.MaxInt); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddRequirement("s1", "r1", "c1"); err != nil {
		t.Fatal(err)
	}
	// 同一要求两次通过，只计一份学分：不超限。
	satisfyByPass(t, s, "s1", "r1", "e1")
	satisfyByPass(t, s, "s1", "r1", "e2")
	if rep := s.CheckStudent("s1"); rep.Overflow || rep.TotalCredits != math.MaxInt {
		t.Fatalf("同一要求多次通过只计一次，不应超限，overflow=%v total=%d",
			rep.Overflow, rep.TotalCredits)
	}
	// 再叠加有效免修，仍只计一份：不超限。
	if _, _, err := s.ApplyWaiver("s1", "r1", "w1", "竞赛获奖"); err != nil {
		t.Fatal(err)
	}
	if rep := s.CheckStudent("s1"); rep.Overflow || rep.TotalCredits != math.MaxInt {
		t.Fatalf("通过修读与有效免修并存只计一次，不应超限，overflow=%v total=%d",
			rep.Overflow, rep.TotalCredits)
	}
}

// 已拒绝或已撤销且没有通过修读的免修不计入总分，不触发超限。
func TestCheckOverflowIgnoresInactiveWaivers(t *testing.T) {
	s := NewStore()
	if _, _, err := s.AddStudent("s1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddCourse("c1", "数学", math.MaxInt); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddCourse("c2", "物理", 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddRequirement("s1", "r1", "c1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddRequirement("s1", "r2", "c2"); err != nil {
		t.Fatal(err)
	}
	satisfyByPass(t, s, "s1", "r1", "e1")

	// r2 的免修被撤销且无通过修读：不计学分，不超限。
	if _, _, err := s.ApplyWaiver("s1", "r2", "w1", "依据"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RevokeWaiver("s1", "w1", "复核不通过"); err != nil {
		t.Fatal(err)
	}
	if rep := s.CheckStudent("s1"); rep.Overflow || rep.TotalCredits != math.MaxInt {
		t.Fatalf("已撤销免修不计学分，不应超限，overflow=%v total=%d",
			rep.Overflow, rep.TotalCredits)
	}

	// r2 的免修被拒绝：同样不计学分。
	s2 := NewStore()
	if _, _, err := s2.AddStudent("s1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s2.AddCourse("c1", "数学", math.MaxInt); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s2.AddCourse("c2", "物理", 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s2.AddRequirement("s1", "r1", "c1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s2.AddRequirement("s1", "r2", "c2"); err != nil {
		t.Fatal(err)
	}
	satisfyByPass(t, s2, "s1", "r1", "e1")
	// 依据为空 → 申请被拒绝并记入历史。
	w, _, err := s2.ApplyWaiver("s1", "r2", "w1", "   ")
	if err != nil {
		t.Fatal(err)
	}
	if w.Status != WaiverRejected {
		t.Fatalf("空依据免修应被拒绝，得到 %s", w.Status)
	}
	if rep := s2.CheckStudent("s1"); rep.Overflow || rep.TotalCredits != math.MaxInt {
		t.Fatalf("已拒绝免修不计学分，不应超限，overflow=%v total=%d",
			rep.Overflow, rep.TotalCredits)
	}
}

// 超限按学生各自判断：另一名学生超限不影响本学生的核对。
func TestCheckOverflowIsPerStudent(t *testing.T) {
	s := NewStore()
	for _, id := range []string{"s1", "s2"} {
		if _, _, err := s.AddStudent(id); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.AddCourse("c1", "数学", math.MaxInt); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddCourse("c2", "物理", 1); err != nil {
		t.Fatal(err)
	}
	// s1 两项都满足，总和超限。
	if _, _, err := s.AddRequirement("s1", "r1", "c1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddRequirement("s1", "r2", "c2"); err != nil {
		t.Fatal(err)
	}
	satisfyByPass(t, s, "s1", "r1", "e1")
	satisfyByPass(t, s, "s1", "r2", "e2")
	// s2 只满足一项，总学分恰好等于上限。
	if _, _, err := s.AddRequirement("s2", "r1", "c1"); err != nil {
		t.Fatal(err)
	}
	satisfyByPass(t, s, "s2", "r1", "e3")

	if rep := s.CheckStudent("s1"); !rep.Overflow {
		t.Fatalf("s1 总学分超限应被标记")
	}
	rep := s.CheckStudent("s2")
	if rep.Overflow || rep.TotalCredits != math.MaxInt {
		t.Fatalf("s2 不受 s1 影响，应正常核对，overflow=%v total=%d",
			rep.Overflow, rep.TotalCredits)
	}
}

// 课程学分接受 int 范围内任意正整数，不设更小的限额。
func TestCourseCreditAcceptsMaxInt(t *testing.T) {
	s := NewStore()
	c, _, err := s.AddCourse("c1", "数学", math.MaxInt)
	if err != nil {
		t.Fatalf("int 上限学分应能登记：%v", err)
	}
	if c.Credit != math.MaxInt {
		t.Fatalf("学分应原样保存为 %d，得到 %d", math.MaxInt, c.Credit)
	}
}
