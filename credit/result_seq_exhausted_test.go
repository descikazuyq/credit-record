package credit

import (
	"math"
	"os"
	"strings"
	"testing"
)

// 本文件回归“成绩提交顺序编号用尽时的首次成绩提交”：计数器 nextResultSeq
// 到达程序整数类型 int 的最大值后，任何仍处于“选课”的修读都不能再首次
// 提交成绩——再递增会回绕成负数并随记录落盘，整份文件下次读取即被判损坏。
// 用例直接操作 Store：
//   - 计数到顶时 pass/fail 首次提交都被拒绝，错误点名学生与修读编号并说明
//     编号已用尽；原修读保持选课、不带序号，记录不发生任何变更；
//   - 该要求另有通过修读或已由有效免修满足，也不放行这次新成绩；
//   - 计数距上限还差一时仍允许一次正常的首次提交（通过、未通过都占用这次
//     机会），序号恰为最大值；
//   - 已提交过结果的修读不需要新序号：重复提交相同结果仍幂等返回，改提
//     另一结果仍按冲突规则拒绝；
//   - 学生或修读不存在时仍报原有的不存在错误。

// setupSeqStore 建立一名学生、一门 4 学分课程与一项要求。
func setupSeqStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "高等数学", 4)
	mustReq(t, s, "s1", "r1", "c1")
	return s
}

// TestSubmitResultSeqExhaustedRejectsFirstSubmission 计数器到达 int 最大值时，
// 对仍处于选课的修读首次提交通过或未通过都必须拒绝：错误点名学生与修读编号、
// 说明顺序编号已用尽；修读保持选课、不获得学分，记录不发生变更。
func TestSubmitResultSeqExhaustedRejectsFirstSubmission(t *testing.T) {
	for _, result := range []Result{Passed, Failed} {
		s := setupSeqStore(t)
		mustEnroll(t, s, "s1", "r1", "2024春", "e1")
		s.nextResultSeq = math.MaxInt
		s.dirty = false // 只观察本次提交是否产生变更

		e, changed, err := s.SubmitResult("s1", "e1", result)
		if err == nil {
			t.Fatalf("结果 %s：编号用尽时首次提交必须被拒绝", result)
		}
		if changed || e != nil {
			t.Fatalf("结果 %s：被拒绝的提交不能返回记录或变更标记，changed=%v e=%v", result, changed, e)
		}
		for _, want := range []string{"s1", "e1", "用尽", "9223372036854775807"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("结果 %s：错误应点名学生、修读并说明编号用尽（缺 %q），err=%q",
					result, want, err)
			}
		}
		if s.dirty {
			t.Fatalf("结果 %s：被拒绝的提交不得改动记录", result)
		}
		got := s.Enrollment("s1", "e1")
		if got.Result != Enrolled || got.ResultSeq != 0 {
			t.Fatalf("结果 %s：原修读应保持选课且不带序号，得到 result=%q seq=%d",
				result, got.Result, got.ResultSeq)
		}
		if s.nextResultSeq != math.MaxInt {
			t.Fatalf("结果 %s：计数器不得回绕或重排，得到 %d", result, s.nextResultSeq)
		}
		rep := s.CheckStudent("s1")
		if rep.TotalCredits != 0 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
			t.Fatalf("结果 %s：被拒绝的提交不得带来学分，total=%d unmet=%v",
				result, rep.TotalCredits, rep.Unmet)
		}
	}
}

// TestSubmitResultSeqExhaustedEvenIfRequirementSatisfied 即使该要求另有通过
// 修读、或已由有效免修满足，编号用尽时仍不能为选课中的修读首次提交成绩。
func TestSubmitResultSeqExhaustedEvenIfRequirementSatisfied(t *testing.T) {
	// 同一要求已有一份通过修读。
	s := setupSeqStore(t)
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	if _, changed, err := s.SubmitResult("s1", "e1", Passed); err != nil || !changed {
		t.Fatalf("首次通过登记失败：changed=%v err=%v", changed, err)
	}
	mustEnroll(t, s, "s1", "r1", "2024秋", "e2")
	s.nextResultSeq = math.MaxInt
	if _, _, err := s.SubmitResult("s1", "e2", Passed); err == nil ||
		!strings.Contains(err.Error(), "用尽") {
		t.Fatalf("要求另有通过修读也不能放行新成绩，err=%v", err)
	}
	if got := s.Enrollment("s1", "e2"); got.Result != Enrolled {
		t.Fatalf("e2 应保持选课，得到 %q", got.Result)
	}

	// 同一要求已由有效免修满足。
	s2 := setupSeqStore(t)
	if _, a, err := s2.ApplyWaiver("s1", "r1", "w1", "竞赛获奖"); err != nil || a != ActionCreated {
		t.Fatalf("有效免修申请失败：action=%v err=%v", a, err)
	}
	mustEnroll(t, s2, "s1", "r1", "2024春", "e1")
	s2.nextResultSeq = math.MaxInt
	if _, _, err := s2.SubmitResult("s1", "e1", Failed); err == nil ||
		!strings.Contains(err.Error(), "用尽") {
		t.Fatalf("有效免修满足要求也不能放行新成绩，err=%v", err)
	}
	if got := s2.Enrollment("s1", "e1"); got.Result != Enrolled {
		t.Fatalf("e1 应保持选课，得到 %q", got.Result)
	}
}

// TestSubmitResultSeqOneBelowMaxAllowsLastSubmission 计数距上限恰好还差一时，
// 允许一次正常的首次提交：通过获得学分、未通过不计学分，序号都恰为最大值；
// 此后计数到顶，下一份选课修读的首次提交被拒绝。
func TestSubmitResultSeqOneBelowMaxAllowsLastSubmission(t *testing.T) {
	s := setupSeqStore(t)
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	mustEnroll(t, s, "s1", "r1", "2024秋", "e2")
	s.nextResultSeq = math.MaxInt - 1

	e, changed, err := s.SubmitResult("s1", "e1", Passed)
	if err != nil || !changed {
		t.Fatalf("距上限差一时应允许最后一次首次提交，changed=%v err=%v", changed, err)
	}
	if e.ResultSeq != math.MaxInt || s.nextResultSeq != math.MaxInt {
		t.Fatalf("最后一次提交应占用序号上限，seq=%d next=%d", e.ResultSeq, s.nextResultSeq)
	}
	if rep := s.CheckStudent("s1"); rep.TotalCredits != 4 {
		t.Fatalf("最后一次通过应正常计学分，total=%d", rep.TotalCredits)
	}
	// 计数到顶：下一份选课修读不能再首次提交。
	if _, _, err := s.SubmitResult("s1", "e2", Failed); err == nil ||
		!strings.Contains(err.Error(), "用尽") {
		t.Fatalf("计数到顶后首次提交未通过也应被拒绝，err=%v", err)
	}

	// 未通过同样占用最后一次机会。
	s2 := setupSeqStore(t)
	mustEnroll(t, s2, "s1", "r1", "2024春", "e1")
	s2.nextResultSeq = math.MaxInt - 1
	e, changed, err = s2.SubmitResult("s1", "e1", Failed)
	if err != nil || !changed || e.ResultSeq != math.MaxInt {
		t.Fatalf("未通过也应允许作为最后一次提交，changed=%v seq=%d err=%v",
			changed, e.ResultSeq, err)
	}
	if rep := s2.CheckStudent("s1"); rep.TotalCredits != 0 {
		t.Fatalf("未通过不计学分，total=%d", rep.TotalCredits)
	}
}

// TestSubmitResultSeqExhaustedKeepsIdempotentAndConflictRules 已提交过结果的
// 修读不需要新序号：计数到顶时重复提交相同结果仍幂等返回原记录、不产生变更；
// 改提另一结果仍按原有冲突规则拒绝。
func TestSubmitResultSeqExhaustedKeepsIdempotentAndConflictRules(t *testing.T) {
	s := setupSeqStore(t)
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	if _, changed, err := s.SubmitResult("s1", "e1", Passed); err != nil || !changed {
		t.Fatalf("首次通过登记失败：changed=%v err=%v", changed, err)
	}
	s.nextResultSeq = math.MaxInt
	s.dirty = false

	e, changed, err := s.SubmitResult("s1", "e1", Passed)
	if err != nil || changed || e == nil || e.Result != Passed {
		t.Fatalf("重复提交相同结果应幂等返回原记录，changed=%v e=%v err=%v", changed, e, err)
	}
	if s.dirty {
		t.Fatal("幂等重复不得产生变更")
	}
	if _, _, err := s.SubmitResult("s1", "e1", Failed); err == nil ||
		!strings.Contains(err.Error(), "不能改为") {
		t.Fatalf("改提另一结果仍应按冲突规则拒绝，err=%v", err)
	}
	if s.dirty {
		t.Fatal("冲突拒绝不得产生变更")
	}
}

// TestSubmitResultSeqExhaustedKeepsNotFoundErrors 计数到顶时，学生或修读
// 不存在仍报原有的不存在错误，与编号用尽无关。
func TestSubmitResultSeqExhaustedKeepsNotFoundErrors(t *testing.T) {
	s := setupSeqStore(t)
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	s.nextResultSeq = math.MaxInt

	if _, _, err := s.SubmitResult("ghost", "e1", Passed); err == nil ||
		!strings.Contains(err.Error(), "学生 ghost 不存在") {
		t.Fatalf("学生不存在应报原有错误，err=%v", err)
	}
	if _, _, err := s.SubmitResult("s1", "ghost", Passed); err == nil ||
		!strings.Contains(err.Error(), "名下不存在修读 ghost") {
		t.Fatalf("修读不存在应报原有错误，err=%v", err)
	}
}

// TestLoadResultSeqExhaustedFileIsLegal 计数器恰为最大值的记录文件是合法
// 记录，不是内容损坏：正常读取、核对与查看，已有结果与提交顺序不变。
func TestLoadResultSeqExhaustedFileIsLegal(t *testing.T) {
	s := setupSeqStore(t)
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	mustEnroll(t, s, "s1", "r1", "2024秋", "e2")
	if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
		t.Fatalf("首次通过登记失败：%v", err)
	}
	s.nextResultSeq = math.MaxInt

	path := t.TempDir() + "/records.json"
	if err := s.Save(path); err != nil {
		t.Fatalf("保存失败：%v", err)
	}
	s2, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("编号用尽的合法文件应正常读取，existed=%v err=%v", existed, err)
	}
	rep := s2.CheckStudent("s1")
	if rep.TotalCredits != 4 || rep.Requirements[0].PassedEnrollmentID != "e1" {
		t.Fatalf("已有结果与来源判定应保持不变，total=%d rep=%v", rep.TotalCredits, rep.Requirements)
	}
	if got := s2.Enrollment("s1", "e2"); got.Result != Enrolled {
		t.Fatalf("未提交的修读应保持选课，得到 %q", got.Result)
	}
	// 读取后首次提交仍被拒绝，且不得改动文件。
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取记录文件失败：%v", err)
	}
	if _, _, err := s2.SubmitResult("s1", "e2", Failed); err == nil {
		t.Fatal("读取后首次提交仍应被拒绝")
	}
	if s2.Dirty() {
		t.Fatal("被拒绝的提交不得产生变更")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取记录文件失败：%v", err)
	}
	if string(after) != string(before) {
		t.Fatal("被拒绝的提交不得改动记录文件")
	}
}
