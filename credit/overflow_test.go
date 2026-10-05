package credit

import (
	"math"
	"strings"
	"testing"
)

// setupOverflowStore 建立两名学生、两门分别为 MaxInt 与 1 学分的课程，
// 以及各自指向两门课程的要求 r1/r2，均不产生修读或免修。
func setupOverflowStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore()
	mustStudent(t, s, "s1")
	mustStudent(t, s, "s2")
	mustCourse(t, s, "c-big", "上限课", math.MaxInt)
	mustCourse(t, s, "c-one", "一学分课", 1)
	mustReq(t, s, "s1", "r1", "c-big")
	mustReq(t, s, "s1", "r2", "c-one")
	mustReq(t, s, "s2", "q1", "c-one")
	return s
}

func passEnrollment(t *testing.T, s *Store, student, req, term, enr string) {
	t.Helper()
	mustEnroll(t, s, student, req, term, enr)
	if _, changed, err := s.SubmitResult(student, enr, Passed); err != nil || !changed {
		t.Fatalf("SubmitResult(%q,%q,passed) = changed %v, err %v", student, enr, changed, err)
	}
}

// TestCheckTotalCreditsOverflowRejected 两门各自合法的课程学分为 MaxInt 与 1，
// 两项要求都满足时总和 MaxInt+1 超出整数范围：核对必须标记 Overflow，
// TotalCredits 不得回绕成负数或任何部分和。
func TestCheckTotalCreditsOverflowRejected(t *testing.T) {
	s := setupOverflowStore(t)
	passEnrollment(t, s, "s1", "r1", "2024春", "e1")
	passEnrollment(t, s, "s1", "r2", "2024春", "e2")

	rep := s.CheckStudent("s1")
	if !rep.Found {
		t.Fatal("学生 s1 应存在")
	}
	if !rep.Overflow {
		t.Fatalf("MaxInt+1 必须判定超限，得到 Overflow=false, Total=%d", rep.TotalCredits)
	}
	if rep.TotalCredits != 0 {
		t.Fatalf("超限时不能给出回绕/截断/部分累加的总学分，得到 %d", rep.TotalCredits)
	}
	msg := rep.OverflowMessage()
	if !strings.Contains(msg, "s1") {
		t.Fatalf("拒绝说明应点名学生编号，得到 %q", msg)
	}
	if !strings.Contains(msg, "9223372036854775807") {
		t.Fatalf("拒绝说明应指出可表示的最大值，得到 %q", msg)
	}
	if strings.Contains(rep.String(), "核对结果") ||
		strings.Contains(rep.String(), "总学分：") {
		t.Fatalf("超限时不能渲染正常核对报告与总学分数值，得到 %q", rep.String())
	}
	// 核对只读：不产生任何额外的待保存变更。
	dirtyBefore := s.Dirty()
	_ = s.CheckStudent("s1")
	if s.Dirty() != dirtyBefore {
		t.Fatal("只读核对超限前后 dirty 状态不应变化")
	}
}

// TestCheckTotalCreditsAtLimitSucceeds 只有 MaxInt 学分的要求满足时，
// 总学分恰好等于上限，仍应正常核对并显示完整数值；另一项只有选课或未通过
// 记录时不参与累计，同样不触发超限。
func TestCheckTotalCreditsAtLimitSucceeds(t *testing.T) {
	// 只有第一项满足：恰为 MaxInt，正常显示完整数值。
	s := setupOverflowStore(t)
	passEnrollment(t, s, "s1", "r1", "2024春", "e1")
	rep := s.CheckStudent("s1")
	if rep.Overflow || rep.TotalCredits != math.MaxInt {
		t.Fatalf("总学分恰为上限时应正常核对，Overflow=%v Total=%d",
			rep.Overflow, rep.TotalCredits)
	}
	if !strings.Contains(rep.String(), "总学分：9223372036854775807") {
		t.Fatalf("应显示完整上限数值，得到 %q", rep.String())
	}

	// 第二项选课：不计学分，不触发超限，且列为未满足要求。
	mustEnroll(t, s, "s1", "r2", "2024春", "e2")
	rep = s.CheckStudent("s1")
	if rep.Overflow || rep.TotalCredits != math.MaxInt {
		t.Fatalf("选课不计学分，总学分仍应为上限，Overflow=%v Total=%d",
			rep.Overflow, rep.TotalCredits)
	}
	if len(rep.Unmet) != 1 || rep.Unmet[0] != "r2" {
		t.Fatalf("r2 只有选课记录应列为未满足，得到 %v", rep.Unmet)
	}

	// 第二项提交未通过：仍不计学分、不触发超限。
	if _, changed, err := s.SubmitResult("s1", "e2", Failed); err != nil || !changed {
		t.Fatalf("提交未通过失败：changed=%v err=%v", changed, err)
	}
	rep = s.CheckStudent("s1")
	if rep.Overflow || rep.TotalCredits != math.MaxInt {
		t.Fatalf("未通过不计学分，总学分仍应为上限，Overflow=%v Total=%d",
			rep.Overflow, rep.TotalCredits)
	}

	// 第二项一旦通过，立即超限。
	passEnrollment(t, s, "s1", "r2", "2025春", "e3")
	if rep = s.CheckStudent("s1"); !rep.Overflow {
		t.Fatalf("第二项通过后 MaxInt+1 必须超限，Total=%d", rep.TotalCredits)
	}
}

// TestCheckOverflowDedupAndWaiverRules 超限判定必须沿用既有计分规则：
// 同一要求多次通过或通过修读与有效免修并存只计一份；已拒绝/已撤销且没有
// 通过修读的免修不计入总分。
func TestCheckOverflowDedupAndWaiverRules(t *testing.T) {
	// 同一 MaxInt 要求通过两次，不与另一门 1 学分课程的满足要求叠加超限。
	s := setupOverflowStore(t)
	passEnrollment(t, s, "s1", "r1", "2024春", "e1")
	passEnrollment(t, s, "s1", "r1", "2025春", "e2")
	// r2 的 1 学分要求保持未满足。
	rep := s.CheckStudent("s1")
	if rep.Overflow || rep.TotalCredits != math.MaxInt {
		t.Fatalf("同一要求多次通过只计一份，不应超限，Overflow=%v Total=%d",
			rep.Overflow, rep.TotalCredits)
	}

	// MaxInt 要求同时有通过修读与有效免修：仍只计一份；1 学分要求未满足。
	if _, a, err := s.ApplyWaiver("s1", "r1", "w1", "竞赛获奖"); err != nil || a != ActionCreated {
		t.Fatalf("有效免修申请失败：action=%v err=%v", a, err)
	}
	rep = s.CheckStudent("s1")
	if rep.Overflow || rep.TotalCredits != math.MaxInt {
		t.Fatalf("通过修读与有效免修并存只计一份，不应超限，Overflow=%v Total=%d",
			rep.Overflow, rep.TotalCredits)
	}

	// 1 学分要求只有已拒绝的免修（目标要求存在但再申请一个指向不存在
	// 要求的被拒免修），不计学分，不超限。
	if w, a, err := s.ApplyWaiver("s1", "rX", "wbad", "依据"); err != nil ||
		a != ActionCreated || w.Status != WaiverRejected {
		t.Fatalf("被拒绝免修登记异常：action=%v status=%v err=%v", a, w.Status, err)
	}
	rep = s.CheckStudent("s1")
	if rep.Overflow || rep.TotalCredits != math.MaxInt {
		t.Fatalf("被拒绝的免修不计学分，不应超限，Overflow=%v Total=%d",
			rep.Overflow, rep.TotalCredits)
	}

	// 撤销 MaxInt 要求的有效免修后有通过修读，仍满足；1 学分要求依旧未满足。
	if _, changed, err := s.RevokeWaiver("s1", "w1", "材料无法核实"); err != nil || !changed {
		t.Fatalf("撤销免修失败：changed=%v err=%v", changed, err)
	}
	rep = s.CheckStudent("s1")
	if rep.Overflow || rep.TotalCredits != math.MaxInt {
		t.Fatalf("撤销后有通过记录的要求继续满足，仍只有一份上限学分，Overflow=%v Total=%d",
			rep.Overflow, rep.TotalCredits)
	}

	// 1 学分要求改为有效免修后才超限（MaxInt 要求由通过修读满足）。
	if _, a, err := s.ApplyWaiver("s1", "r2", "w2", "外校课程"); err != nil || a != ActionCreated {
		t.Fatalf("r2 有效免修申请失败：action=%v err=%v", a, err)
	}
	if rep = s.CheckStudent("s1"); !rep.Overflow || rep.TotalCredits != 0 {
		t.Fatalf("两项要求都满足时必须超限且不给部分和，Overflow=%v Total=%d",
			rep.Overflow, rep.TotalCredits)
	}
}

// TestCheckOverflowIsPerStudent 只有实际超限的学生被拒绝：另一名学生取得的
// 学分完全不参与，其正常核对（包括恰好等于上限的总学分）不受影响。
func TestCheckOverflowIsPerStudent(t *testing.T) {
	s := setupOverflowStore(t)
	// s1 两项都满足，必然超限。
	passEnrollment(t, s, "s1", "r1", "2024春", "e1")
	passEnrollment(t, s, "s1", "r2", "2024春", "e2")
	// s2 只有一门 1 学分要求，正常核对。
	passEnrollment(t, s, "s2", "q1", "2024春", "f1")

	rep1 := s.CheckStudent("s1")
	rep2 := s.CheckStudent("s2")
	if !rep1.Overflow {
		t.Fatal("s1 应超限")
	}
	if rep2.Overflow || rep2.TotalCredits != 1 {
		t.Fatalf("s2 不应受 s1 影响，Overflow=%v Total=%d", rep2.Overflow, rep2.TotalCredits)
	}
}
