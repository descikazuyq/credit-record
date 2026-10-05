package credit

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归“首次提交成绩时从全局递增计数器领取结果提交序号”这一既有
// 机制在计数器达到整数类型上限时的边界：
//   - 计数器已达 int 最大值（64 位环境下 9223372036854775807）时，任何
//     仍处于“选课”的修读都不能再首次提交通过或未通过：明确拒绝且不改动
//     修读结果、序号、计数器与学分；
//   - 距上限恰好还差一时允许最后一次正常的首次提交（通过、未通过都覆盖），
//     最后一次提交的先后关系照常保留，之后的新提交即被拒绝；
//   - 已提交过结果的修读不需要新序号：计数器到上限后重复提交相同结果仍
//     幂等返回原记录，改提另一结果仍按冲突规则拒绝；
//   - 即使该要求已有另一份通过修读、或已由有效免修满足，也不能放行一次
//     新的首次成绩提交；
//   - 学生或修读不存在时继续走原有的不存在报错，不与“编号用尽”混淆；
//   - 编号已用尽本身不使合法记录变成内容损坏：show 所用的查询与核对照常
//     工作，已有学分、免修状态与来源判定不变；拒绝不标记变更、不落盘。

// setupSeqLimitStore 建立学生 s1（必要时加 s2）、4 学分课程 c1 与要求
// r1，不产生任何修读或结果。
func setupSeqLimitStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "高等数学", 4)
	mustReq(t, s, "s1", "r1", "c1")
	return s
}

// assertSubmitSeqLimitError 断言一次首次提交被“序号用尽”拒绝：错误点名
// 学生、修读并说明顺序编号已用尽、无法提交新成绩。
func assertSubmitSeqLimitError(t *testing.T, err error, student, enr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("学生 %s 的修读 %s 在序号用尽后首次提交必须被拒绝", student, enr)
	}
	msg := err.Error()
	for _, want := range []string{
		student,
		enr,
		"成绩提交顺序编号已用尽",
		"无法提交新的成绩",
		"9223372036854775807",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("拒绝信息应包含 %q，得到 %q", want, msg)
		}
	}
}

// TestSubmitResultSeqExhaustedRejectsNewSubmission 计数器已达 int 上限时，
// 对仍处于选课的修读首次提交通过或未通过都必须拒绝：退出错误、changed
// 为 false，修读保持选课、不获得学分，计数器不发生回绕，记录不标记变更。
func TestSubmitResultSeqExhaustedRejectsNewSubmission(t *testing.T) {
	for _, result := range []Result{Passed, Failed} {
		t.Run(string(result), func(t *testing.T) {
			s := setupSeqLimitStore(t)
			mustEnroll(t, s, "s1", "r1", "2024春", "e1")
			s.nextResultSeq = math.MaxInt
			dirtyBefore := s.Dirty()

			e, changed, err := s.SubmitResult("s1", "e1", result)
			assertSubmitSeqLimitError(t, err, "s1", "e1")
			if changed || e != nil {
				t.Fatalf("序号用尽时不应返回被修改的修读或报告变更，e=%v changed=%v", e, changed)
			}
			// 计数器绝不能回绕成负数。
			if s.nextResultSeq != math.MaxInt {
				t.Fatalf("拒绝后计数器应保持 MaxInt，得到 %d", s.nextResultSeq)
			}
			got := s.Enrollment("s1", "e1")
			if got.Result != Enrolled || got.ResultSeq != 0 {
				t.Fatalf("修读应保持选课且无序号，得到 %+v", got)
			}
			if s.Dirty() != dirtyBefore {
				t.Fatal("被拒绝的提交不应新增任何待保存变更")
			}
			rep := s.CheckStudent("s1")
			if rep.TotalCredits != 0 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
				t.Fatalf("拒绝后要求仍应未满足、0 学分，得到 %+v", rep)
			}
		})
	}
}

// TestSubmitResultSeqOneBelowLimitAllowsFinalSubmission 计数距上限恰好还差
// 一时，应允许一次正常的首次提交并把 MaxInt 作为其提交序号，保留这次最后
// 合法提交的先后关系；通过计学分、未通过不计。提交后计数器到顶，任何新
// 的首次提交都被拒绝。
func TestSubmitResultSeqOneBelowLimitAllowsFinalSubmission(t *testing.T) {
	t.Run("最后一次是通过", func(t *testing.T) {
		s := setupSeqLimitStore(t)
		// 先有一份较早的未通过 e1（序号 MaxInt-2），再留两个待提交的选课。
		mustEnroll(t, s, "s1", "r1", "2024春", "e1")
		mustEnroll(t, s, "s1", "r1", "2024秋", "e2")
		s.nextResultSeq = math.MaxInt - 2
		if _, changed, err := s.SubmitResult("s1", "e1", Failed); err != nil || !changed {
			t.Fatalf("序号 MaxInt-2 的未通过提交应成功，changed=%v err=%v", changed, err)
		}
		// 距上限恰好还差一：最后一次合法的首次提交。
		e, changed, err := s.SubmitResult("s1", "e2", Passed)
		if err != nil || !changed {
			t.Fatalf("距上限还差一时应允许最后一次首次提交，changed=%v err=%v", changed, err)
		}
		if e.ResultSeq != math.MaxInt || s.nextResultSeq != math.MaxInt {
			t.Fatalf("最后一次提交应取得序号 MaxInt，修读=%d 计数器=%d",
				e.ResultSeq, s.nextResultSeq)
		}
		// 先后关系保留：较早的未通过仍不是来源，通过的 e2 说明来源。
		rep := s.CheckStudent("s1")
		if rep.TotalCredits != 4 || rep.Requirements[0].PassedEnrollmentID != "e2" {
			t.Fatalf("最后一次通过应计 4 学分并说明来源，得到 %+v", rep)
		}

		// 到顶后新选课的首次提交被拒绝，已有结果不变。
		mustEnroll(t, s, "s1", "r1", "2025春", "e3")
		_, changed, err = s.SubmitResult("s1", "e3", Passed)
		assertSubmitSeqLimitError(t, err, "s1", "e3")
		if changed {
			t.Fatal("到顶后的提交不应产生变更")
		}
		if e3 := s.Enrollment("s1", "e3"); e3.Result != Enrolled || e3.ResultSeq != 0 {
			t.Fatalf("被拒绝的 e3 应保持选课，得到 %+v", e3)
		}
		if e2 := s.Enrollment("s1", "e2"); e2.Result != Passed || e2.ResultSeq != math.MaxInt {
			t.Fatalf("最后一次合法提交 e2 的结果与序号应保留，得到 %+v", e2)
		}
	})

	t.Run("最后一次是未通过", func(t *testing.T) {
		s := setupSeqLimitStore(t)
		mustEnroll(t, s, "s1", "r1", "2024春", "e1")
		mustEnroll(t, s, "s1", "r1", "2024秋", "e2")
		s.nextResultSeq = math.MaxInt - 1
		// 最后一次合法提交给了未通过：未通过同样占用这次机会。
		e, changed, err := s.SubmitResult("s1", "e1", Failed)
		if err != nil || !changed || e.ResultSeq != math.MaxInt {
			t.Fatalf("距上限还差一时未通过也应成功并取得 MaxInt 序号，e=%+v changed=%v err=%v",
				e, changed, err)
		}
		if s.nextResultSeq != math.MaxInt {
			t.Fatalf("计数器应到顶，得到 %d", s.nextResultSeq)
		}
		// 未通过仍不计学分，要求未满足。
		rep := s.CheckStudent("s1")
		if rep.TotalCredits != 0 || len(rep.Unmet) != 1 {
			t.Fatalf("最后一次未通过不应计学分，得到 %+v", rep)
		}
		// 另一份选课再也无法首次提交，即使提交的是通过。
		_, _, err = s.SubmitResult("s1", "e2", Passed)
		assertSubmitSeqLimitError(t, err, "s1", "e2")
		if e2 := s.Enrollment("s1", "e2"); e2.Result != Enrolled || e2.ResultSeq != 0 {
			t.Fatalf("e2 应保持选课，得到 %+v", e2)
		}
	})
}

// TestSubmitResultSeqExhaustedIdempotentAndConflictRules 已提交过结果的修读
// 不需要新序号：计数器到顶后，重复提交相同结果仍幂等返回原记录、不增学分、
// 不标记变更；改提另一结果仍按原有冲突规则拒绝。
func TestSubmitResultSeqExhaustedIdempotentAndConflictRules(t *testing.T) {
	s := setupSeqLimitStore(t)
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	mustEnroll(t, s, "s1", "r1", "2024秋", "e2")
	s.nextResultSeq = math.MaxInt - 2
	if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SubmitResult("s1", "e2", Failed); err != nil {
		t.Fatal(err)
	}
	// 此刻计数器到顶；e1 通过（MaxInt-1）、e2 未通过（MaxInt）。

	// 重复提交相同结果：幂等成功、changed=false，计数器仍是 MaxInt。
	if e, changed, err := s.SubmitResult("s1", "e1", Passed); err != nil || changed ||
		e.Result != Passed || e.ResultSeq != math.MaxInt-1 {
		t.Fatalf("到顶后重复通过应幂等返回原记录，e=%+v changed=%v err=%v", e, changed, err)
	}
	if e, changed, err := s.SubmitResult("s1", "e2", Failed); err != nil || changed ||
		e.Result != Failed || e.ResultSeq != math.MaxInt {
		t.Fatalf("到顶后重复未通过应幂等返回原记录，e=%+v changed=%v err=%v", e, changed, err)
	}
	if s.nextResultSeq != math.MaxInt {
		t.Fatalf("幂等重复不应改变计数器，得到 %d", s.nextResultSeq)
	}
	// 学分仍只有一份 4。
	if rep := s.CheckStudent("s1"); rep.TotalCredits != 4 {
		t.Fatalf("幂等重复不应增加学分，得到 %d", rep.TotalCredits)
	}

	// 改提另一结果：按原有冲突规则拒绝（不是“编号用尽”），原结果保留。
	_, _, err := s.SubmitResult("s1", "e1", Failed)
	if err == nil || !strings.Contains(err.Error(), "不能改为") {
		t.Fatalf("通过改未通过应按冲突规则拒绝，得到 %v", err)
	}
	if strings.Contains(err.Error(), "编号已用尽") {
		t.Fatalf("改结果冲突不应报成编号用尽，得到 %v", err)
	}
	_, _, err = s.SubmitResult("s1", "e2", Passed)
	if err == nil || !strings.Contains(err.Error(), "不能改为") {
		t.Fatalf("未通过改通过应按冲突规则拒绝，得到 %v", err)
	}
	if e1 := s.Enrollment("s1", "e1"); e1.Result != Passed {
		t.Fatalf("e1 应保留通过，得到 %+v", e1)
	}
	if e2 := s.Enrollment("s1", "e2"); e2.Result != Failed {
		t.Fatalf("e2 应保留未通过，得到 %+v", e2)
	}
}

// TestSubmitResultSeqExhaustedNotBypassedByExistingPassOrWaiver 即使该要求
// 已有另一份通过修读、或已由有效免修满足，计数器到顶后仍处于选课的修读
// 也不能首次提交成绩：编号用尽是独立的边界，不被既有满足状态放行。
func TestSubmitResultSeqExhaustedNotBypassedByExistingPassOrWaiver(t *testing.T) {
	t.Run("同一要求已有另一份通过", func(t *testing.T) {
		s := setupSeqLimitStore(t)
		mustEnroll(t, s, "s1", "r1", "2024春", "e1")
		mustEnroll(t, s, "s1", "r1", "2024秋", "e2")
		s.nextResultSeq = math.MaxInt - 1
		if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
			t.Fatal(err)
		}
		// 要求已由 e1 满足，e2 仍是选课：到顶后 e2 的通过仍被拒绝。
		_, _, err := s.SubmitResult("s1", "e2", Passed)
		assertSubmitSeqLimitError(t, err, "s1", "e2")
		if e2 := s.Enrollment("s1", "e2"); e2.Result != Enrolled {
			t.Fatalf("e2 应保持选课，得到 %+v", e2)
		}
		if rep := s.CheckStudent("s1"); rep.TotalCredits != 4 ||
			rep.Requirements[0].PassedEnrollmentID != "e1" ||
			len(rep.Requirements[0].PassedEnrollmentIDs) != 1 {
			t.Fatalf("拒绝后来源应仍是 e1、通过历史只有一条，得到 %+v", rep)
		}
	})

	t.Run("要求已由有效免修满足", func(t *testing.T) {
		s := setupSeqLimitStore(t)
		if _, a, err := s.ApplyWaiver("s1", "r1", "w1", "学科竞赛获奖"); err != nil ||
			a != ActionCreated {
			t.Fatalf("有效免修应成功，action=%v err=%v", a, err)
		}
		mustEnroll(t, s, "s1", "r1", "2024春", "e1")
		s.nextResultSeq = math.MaxInt
		_, _, err := s.SubmitResult("s1", "e1", Passed)
		assertSubmitSeqLimitError(t, err, "s1", "e1")
		if e1 := s.Enrollment("s1", "e1"); e1.Result != Enrolled {
			t.Fatalf("e1 应保持选课，得到 %+v", e1)
		}
		// 免修状态与来源判定不变。
		rep := s.CheckStudent("s1")
		if rep.TotalCredits != 4 || rep.Requirements[0].Source != "waiver" ||
			rep.Requirements[0].WaiverID != "w1" {
			t.Fatalf("有效免修仍应满足要求，得到 %+v", rep)
		}
	})
}

// TestSubmitResultSeqExhaustedUnknownStudentAndEnrollment 序号到顶后，学生
// 或本人名下修读不存在时继续给出原有的不存在提示，不与编号用尽混淆，也
// 不改动任何记录。
func TestSubmitResultSeqExhaustedUnknownStudentAndEnrollment(t *testing.T) {
	s := setupSeqLimitStore(t)
	mustStudent(t, s, "s2")
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	s.nextResultSeq = math.MaxInt
	dirtyBefore := s.Dirty()

	if _, _, err := s.SubmitResult("ghost", "e1", Passed); err == nil ||
		!strings.Contains(err.Error(), "学生 ghost 不存在") {
		t.Fatalf("不存在学生应走原有报错，得到 %v", err)
	}
	if _, _, err := s.SubmitResult("s1", "nope", Passed); err == nil ||
		!strings.Contains(err.Error(), "学生 s1 名下不存在修读 nope") {
		t.Fatalf("不存在修读应走原有报错，得到 %v", err)
	}
	// s2 名下没有该修读：不能借用 s1 的 e1。
	if _, _, err := s.SubmitResult("s2", "e1", Passed); err == nil ||
		!strings.Contains(err.Error(), "学生 s2 名下不存在修读 e1") {
		t.Fatalf("借名提交应按本人名下不存在报错，得到 %v", err)
	}
	if e1 := s.Enrollment("s1", "e1"); e1.Result != Enrolled {
		t.Fatalf("不存在类报错不应影响 s1 的选课 e1，得到 %+v", e1)
	}
	if s.Dirty() != dirtyBefore {
		t.Fatal("不存在类报错不应新增任何待保存变更")
	}
}

// TestSubmitResultSeqExhaustedRecordStaysValidAndUnchanged 编号用尽本身不
// 使合法记录变成内容损坏：落盘后仍可正常读取，查询与核对继续遵守原有
// 规则，已有结果与提交顺序不变；被拒绝的提交不落盘。
func TestSubmitResultSeqExhaustedRecordStaysValidAndUnchanged(t *testing.T) {
	s := setupSeqLimitStore(t)
	mustStudent(t, s, "s2")
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	mustEnroll(t, s, "s1", "r1", "2024夏", "e2")
	mustEnroll(t, s, "s1", "r1", "2024秋", "e3")
	// e1 通过取 MaxInt-1，e2 未通过取 MaxInt（未通过同样占用序号），
	// 计数器随即到顶；e3 仍选课，它的首次提交被拒绝。
	s.nextResultSeq = math.MaxInt - 2
	if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SubmitResult("s1", "e2", Failed); err != nil {
		t.Fatal(err)
	}
	_, _, rejectErr := s.SubmitResult("s1", "e3", Failed)
	assertSubmitSeqLimitError(t, rejectErr, "s1", "e3")

	path := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// 到顶的计数器与 MaxInt 序号都是合法文件内容：必须正常读取。
	loaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("编号到顶的合法记录应可正常读取，existed=%v err=%v", existed, err)
	}
	if loaded.nextResultSeq != math.MaxInt {
		t.Fatalf("计数器应原样恢复为 MaxInt，得到 %d", loaded.nextResultSeq)
	}
	if e1 := loaded.Enrollment("s1", "e1"); e1 == nil || e1.Result != Passed ||
		e1.ResultSeq != math.MaxInt-1 {
		t.Fatalf("e1 的通过与序号应原样恢复，得到 %+v", e1)
	}
	if e2 := loaded.Enrollment("s1", "e2"); e2 == nil || e2.Result != Failed ||
		e2.ResultSeq != math.MaxInt {
		t.Fatalf("e2 的未通过与最后一个序号应原样恢复，得到 %+v", e2)
	}
	if e3 := loaded.Enrollment("s1", "e3"); e3 == nil || e3.Result != Enrolled {
		t.Fatalf("e3 应仍为选课，得到 %+v", e3)
	}
	rep := loaded.CheckStudent("s1")
	if rep.TotalCredits != 4 || rep.Requirements[0].PassedEnrollmentID != "e1" {
		t.Fatalf("读取后核对规则不变，得到 %+v", rep)
	}

	// 重新加载后再次尝试首次提交：仍拒绝，且整份记录文件保持原样。
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = loaded.SubmitResult("s1", "e3", Passed)
	assertSubmitSeqLimitError(t, err, "s1", "e3")
	if loaded.Dirty() {
		t.Fatal("只读加载后的拒绝不应标记变更")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatalf("拒绝提交不得改写记录文件\nwant=%q\n got=%q", raw, got)
	}
}
