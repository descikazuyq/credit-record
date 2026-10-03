package credit

import (
	"strings"
	"testing"
)

func mustStudent(t *testing.T, s *Store, id string) {
	t.Helper()
	if _, a, err := s.AddStudent(id); err != nil || a != ActionCreated {
		t.Fatalf("AddStudent(%q) = action %v, err %v", id, a, err)
	}
}

func mustCourse(t *testing.T, s *Store, id, name string, credit int) {
	t.Helper()
	if _, a, err := s.AddCourse(id, name, credit); err != nil || a != ActionCreated {
		t.Fatalf("AddCourse(%q) = action %v, err %v", id, a, err)
	}
}

func mustReq(t *testing.T, s *Store, st, r, c string) {
	t.Helper()
	if _, a, err := s.AddRequirement(st, r, c); err != nil || a != ActionCreated {
		t.Fatalf("AddRequirement(%q,%q,%q) = action %v, err %v", st, r, c, a, err)
	}
}

func mustEnroll(t *testing.T, s *Store, st, r, term, e string) {
	t.Helper()
	if _, a, err := s.AddEnrollment(st, r, term, e); err != nil || a != ActionCreated {
		t.Fatalf("AddEnrollment(%q,%q,%q,%q) = action %v, err %v", st, r, term, e, a, err)
	}
}

func TestStudentAndCourseBasics(t *testing.T) {
	s := NewStore()

	if _, _, err := s.AddStudent("  "); err == nil {
		t.Fatal("空白学生编号应当被拒绝")
	}
	mustStudent(t, s, "s1")
	// 重复登记学生幂等。
	if _, a, err := s.AddStudent("s1"); err != nil || a != ActionExisted {
		t.Fatalf("重复登记学生应幂等，得到 action=%v err=%v", a, err)
	}

	// 学分必须正整数。
	if _, _, err := s.AddCourse("c1", "数学", 0); err == nil {
		t.Fatal("学分 0 应被拒绝")
	}
	if _, _, err := s.AddCourse("c1", "数学", -3); err == nil {
		t.Fatal("负数学分应被拒绝")
	}
	mustCourse(t, s, "c1", "数学", 4)

	// 同编号同内容幂等。
	c, a, err := s.AddCourse("c1", "数学", 4)
	if err != nil || a != ActionExisted || !c.Open {
		t.Fatalf("重复登记课程应幂等且保持开放，得到 %v %v %v", c, a, err)
	}
}

func TestCourseCreditChangeGuardedByReference(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "数学", 4)

	// 未被引用时允许改学分。
	if _, a, err := s.AddCourse("c1", "高等数学", 5); err != nil || a != ActionUpdated {
		t.Fatalf("未被引用的课程应允许修改，得到 action=%v err=%v", a, err)
	}

	mustReq(t, s, "s1", "r1", "c1")
	// 被要求引用后不能改学分。
	if _, _, err := s.AddCourse("c1", "高等数学", 6); err == nil {
		t.Fatal("已被要求引用的课程不应允许修改学分")
	}
	if got := s.Course("c1").Credit; got != 5 {
		t.Fatalf("拒绝修改后学分应保留为 5，得到 %d", got)
	}
}

func TestCourseCloseBlocksNewEnrollmentOnly(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "数学", 4)
	mustReq(t, s, "s1", "r1", "c1")
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")

	if _, err := s.SetCourseOpen("c1", false); err != nil {
		t.Fatal(err)
	}
	// 停开后不能新增修读。
	if _, _, err := s.AddEnrollment("s1", "r1", "2024秋", "e2"); err == nil {
		t.Fatal("课程停开后不应允许新增修读")
	}
	// 已有修读仍可提交结果。
	if _, changed, err := s.SubmitResult("s1", "e1", Passed); err != nil || !changed {
		t.Fatalf("停开后已有修读应能提交通过，changed=%v err=%v", changed, err)
	}
	// 恢复开放后又可新增。
	if _, err := s.SetCourseOpen("c1", true); err != nil {
		t.Fatal(err)
	}
	mustEnroll(t, s, "s1", "r1", "2024秋", "e2")
}

func TestRequirementRules(t *testing.T) {
	s := NewStore()
	mustCourse(t, s, "c1", "数学", 4)
	mustCourse(t, s, "c2", "物理", 3)
	mustStudent(t, s, "s1")
	mustStudent(t, s, "s2")

	// 学生不存在。
	if _, _, err := s.AddRequirement("nobody", "r1", "c1"); err == nil {
		t.Fatal("引用不存在的学生应被拒绝")
	}
	// 课程不存在。
	if _, _, err := s.AddRequirement("s1", "r1", "nope"); err == nil {
		t.Fatal("引用不存在的课程应被拒绝")
	}
	mustReq(t, s, "s1", "r1", "c1")
	// 同学生同课程不能重复建立要求（即使编号不同）。
	if _, _, err := s.AddRequirement("s1", "r2", "c1"); err == nil {
		t.Fatal("同一学生就同一课程不应重复建立要求")
	}
	// 同编号改指课程应拒绝。
	if _, _, err := s.AddRequirement("s1", "r1", "c2"); err == nil {
		t.Fatal("同编号要求不应改指课程")
	}
	if got := s.Requirement("s1", "r1").CourseID; got != "c1" {
		t.Fatalf("原要求应保留指向 c1，得到 %s", got)
	}
	// 重复相同内容幂等。
	if _, a, err := s.AddRequirement("s1", "r1", "c1"); err != nil || a != ActionExisted {
		t.Fatalf("重复要求应幂等，得到 action=%v err=%v", a, err)
	}
	// 要求不跨学生共享：s2 可以就同一课程建立同编号要求。
	mustReq(t, s, "s2", "r1", "c1")
}

func TestEnrollmentRules(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "数学", 4)
	mustReq(t, s, "s1", "r1", "c1")

	if _, _, err := s.AddEnrollment("s1", "r1", "2024春", " "); err == nil {
		t.Fatal("空白修读编号应被拒绝")
	}
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	// 同一学期允许多次修读，靠不同编号区分。
	mustEnroll(t, s, "s1", "r1", "2024春", "e2")
	// 相同内容重复登记返回原记录。
	if _, a, err := s.AddEnrollment("s1", "r1", "2024春", "e1"); err != nil || a != ActionExisted {
		t.Fatalf("重复修读登记应幂等，得到 action=%v err=%v", a, err)
	}
	// 相同编号换要求。
	mustCourse(t, s, "c2", "物理", 3)
	mustReq(t, s, "s1", "r2", "c2")
	if _, _, err := s.AddEnrollment("s1", "r2", "2024春", "e1"); err == nil {
		t.Fatal("相同修读编号换要求应被拒绝")
	}
	// 相同编号换学期。
	if _, _, err := s.AddEnrollment("s1", "r1", "2024秋", "e1"); err == nil {
		t.Fatal("相同修读编号换学期应被拒绝")
	}
	// 引用不存在的要求。
	if _, _, err := s.AddEnrollment("s1", "nope", "2024春", "e3"); err == nil {
		t.Fatal("引用不存在要求的修读应被拒绝")
	}
}

func TestResultSubmissionRules(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "数学", 4)
	mustReq(t, s, "s1", "r1", "c1")
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")

	if e := s.Enrollment("s1", "e1"); e.Result != Enrolled {
		t.Fatalf("新修读应为选课，得到 %s", e.Result)
	}
	// 非法结果。
	if _, _, err := s.SubmitResult("s1", "e1", Enrolled); err == nil {
		t.Fatal("提交选课不应是合法结果提交")
	}
	// 提交未通过。
	if _, changed, err := s.SubmitResult("s1", "e1", Failed); err != nil || !changed {
		t.Fatalf("提交未通过失败：changed=%v err=%v", changed, err)
	}
	// 已提交结果后改成另一结果应拒绝。
	if _, _, err := s.SubmitResult("s1", "e1", Passed); err == nil {
		t.Fatal("未通过后改提通过应被拒绝")
	}
	if got := s.Enrollment("s1", "e1").Result; got != Failed {
		t.Fatalf("原结果应保留为未通过，得到 %s", got)
	}
	// 重复提交相同结果返回原记录。
	if _, changed, err := s.SubmitResult("s1", "e1", Failed); err != nil || changed {
		t.Fatalf("重复相同结果应幂等，changed=%v err=%v", changed, err)
	}
	// 不存在的修读。
	if _, _, err := s.SubmitResult("s1", "nope", Passed); err == nil {
		t.Fatal("对不存在的修读提交结果应报错")
	}
}

func TestMultiplePassesCountOnceWithEarliestSource(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "数学", 4)
	mustReq(t, s, "s1", "r1", "c1")
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	mustEnroll(t, s, "s1", "r1", "2024秋", "e2")

	// e2 先提交通过，e1 后提交通过。
	if _, _, err := s.SubmitResult("s1", "e2", Passed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
		t.Fatal(err)
	}

	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 4 {
		t.Fatalf("多次通过只应计一份学分，总学分=%d", rep.TotalCredits)
	}
	st := rep.Requirements[0]
	if st.Source != "enrollment" || st.PassedEnrollmentID != "e2" {
		t.Fatalf("无有效免修时应以最先提交的通过记录 e2 说明来源，得到 source=%s id=%s",
			st.Source, st.PassedEnrollmentID)
	}
	if len(st.PassedEnrollmentIDs) != 2 {
		t.Fatalf("修读历史应保留两次通过，得到 %v", st.PassedEnrollmentIDs)
	}
}

// TestRevokeWaiverRestoresEarliestSubmittedPassWithRepeatedEnrollment 一项 4
// 学分要求已有两次通过修读（同一学期、编号不同，登记次序与通过结果提交次序
// 相反），事后取得有效免修：免修有效时只计一份学分且来源是免修，两次通过
// 都保留在修读历史中；填写原因撤销后，要求继续满足、总学分不变，来源恢复为
// 最先提交通过结果的那次修读，而不是按学期、编号或选课登记次序挑选；重复
// 撤销给出不同原因不能覆盖首次撤销原因，也不能改变已恢复的来源。
func TestRevokeWaiverRestoresEarliestSubmittedPassWithRepeatedEnrollment(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "高等数学", 4)
	mustReq(t, s, "s1", "r1", "c1")
	// 同一学期两次修读：e1 先登记、e2 后登记。
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	mustEnroll(t, s, "s1", "r1", "2024春", "e2")

	// 通过结果提交次序与登记次序相反：后登记的 e2 先提交通过。
	if _, _, err := s.SubmitResult("s1", "e2", Passed); err != nil {
		t.Fatal(err)
	}
	if e2 := s.Enrollment("s1", "e2"); e2.ResultSeq == 0 {
		t.Fatalf("e2 提交通过后应带结果序号，得到 %+v", e2)
	}
	if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
		t.Fatal(err)
	}
	if e1 := s.Enrollment("s1", "e1"); e1.ResultSeq <= s.Enrollment("s1", "e2").ResultSeq {
		t.Fatalf("e1 的提交序号应大于 e2，得到 e1=%d e2=%d",
			e1.ResultSeq, s.Enrollment("s1", "e2").ResultSeq)
	}

	// 免修前：4 学分，来源是最先提交的 e2，不是编号/登记次序在前的 e1。
	repBefore := s.CheckStudent("s1")
	if repBefore.TotalCredits != 4 || len(repBefore.Unmet) != 0 {
		t.Fatalf("免修前应满足且 4 学分，得到学分=%d 未满足=%v",
			repBefore.TotalCredits, repBefore.Unmet)
	}
	stBefore := repBefore.Requirements[0]
	if stBefore.Source != "enrollment" || stBefore.PassedEnrollmentID != "e2" {
		t.Fatalf("免修前来源应为最先提交通过的 e2，得到 %+v", stBefore)
	}
	if got := stBefore.PassedEnrollmentIDs; len(got) != 2 || got[0] != "e2" || got[1] != "e1" {
		t.Fatalf("通过历史应按提交先后为 [e2 e1]，得到 %v", got)
	}

	// 事后取得有效免修。
	w, a, err := s.ApplyWaiver("s1", "r1", "w1", "学科竞赛获奖")
	if err != nil || a != ActionCreated || w.Status != WaiverApproved {
		t.Fatalf("免修应生效，得到 %+v action=%v err=%v", w, a, err)
	}
	repWaiver := s.CheckStudent("s1")
	if repWaiver.TotalCredits != 4 {
		t.Fatalf("免修与两次通过并存只应计一份 4 学分，得到 %d", repWaiver.TotalCredits)
	}
	stW := repWaiver.Requirements[0]
	if stW.Source != "waiver" || stW.WaiverID != "w1" || stW.PassedEnrollmentID != "" {
		t.Fatalf("免修有效时应以免修说明来源，得到 %+v", stW)
	}
	if got := stW.PassedEnrollmentIDs; len(got) != 2 || got[0] != "e2" || got[1] != "e1" {
		t.Fatalf("免修有效时两次通过仍应保留在修读历史中，得到 %v", got)
	}

	// 填写原因撤销：要求继续满足、总学分不变，来源恢复为最先提交的 e2。
	revokeReason := "证明材料复核不通过"
	rw, changed, err := s.RevokeWaiver("s1", "w1", revokeReason)
	if err != nil || !changed || rw.Status != WaiverRevoked {
		t.Fatalf("撤销应成功，changed=%v 得到 %+v err=%v", changed, rw, err)
	}
	repAfter := s.CheckStudent("s1")
	if repAfter.TotalCredits != 4 || len(repAfter.Unmet) != 0 {
		t.Fatalf("撤销后应继续满足且总学分不变，得到学分=%d 未满足=%v",
			repAfter.TotalCredits, repAfter.Unmet)
	}
	stAfter := repAfter.Requirements[0]
	if stAfter.Satisfied != stBefore.Satisfied || stAfter.Source != stBefore.Source ||
		stAfter.PassedEnrollmentID != stBefore.PassedEnrollmentID {
		t.Fatalf("撤销后来源应恢复为免修前的判定，免修前 %+v，撤销后 %+v",
			stBefore, stAfter)
	}
	if got := stAfter.PassedEnrollmentIDs; len(got) != 2 || got[0] != "e2" || got[1] != "e1" {
		t.Fatalf("撤销后两次通过的提交先后应保持原样，得到 %v", got)
	}
	if len(repAfter.RevokedWaivers) != 1 || repAfter.RevokedWaivers[0] != "w1" {
		t.Fatalf("核对应列出已撤销免修 w1，得到 %v", repAfter.RevokedWaivers)
	}
	// 免修历史保留原编号、指向要求、原依据与本次撤销原因。
	if got := s.Waiver("s1", "w1"); got.Status != WaiverRevoked || got.ReqID != "r1" ||
		got.Basis != "学科竞赛获奖" || got.Reason != revokeReason {
		t.Fatalf("免修历史应原样保留并记录撤销原因，得到 %+v", got)
	}
	// 通过修读的结果与提交序号保持原样。
	if e := s.Enrollment("s1", "e1"); e.Result != Passed || e.Term != "2024春" {
		t.Fatalf("e1 应保持原通过结果，得到 %+v", e)
	}
	if e := s.Enrollment("s1", "e2"); e.Result != Passed || e.Term != "2024春" {
		t.Fatalf("e2 应保持原通过结果，得到 %+v", e)
	}

	// 用不同原因重复撤销：返回原结果，首次原因与已恢复的来源都不变。
	rw2, changed2, err := s.RevokeWaiver("s1", "w1", "后来补充的另一个原因")
	if err != nil || changed2 || rw2.Reason != revokeReason {
		t.Fatalf("重复撤销应原样返回且不覆盖原因，changed=%v 得到 %+v err=%v",
			changed2, rw2, err)
	}
	repRepeat := s.CheckStudent("s1")
	stRepeat := repRepeat.Requirements[0]
	if repRepeat.TotalCredits != 4 || stRepeat.Source != "enrollment" ||
		stRepeat.PassedEnrollmentID != "e2" {
		t.Fatalf("重复撤销后学分来源应仍为最先提交的 e2，得到 %+v", stRepeat)
	}
	if got := s.Waiver("s1", "w1"); got.Status != WaiverRevoked || got.Reason != revokeReason {
		t.Fatalf("首次撤销原因应保留，得到 %+v", got)
	}
}

// TestRevokeWaiverWithoutPassReturnsUnmetAndKeepsOtherRequirements 撤销前只有
// 选课与未通过修读、没有任何通过记录时，撤销免修后该要求回到未满足并失去
// 原先提供的一份课程学分；已撤销免修与未通过修读都不能再作为来源，选课与
// 未通过记录仍可查；同一名学生其他要求的满足情况与学分不变。
func TestRevokeWaiverWithoutPassReturnsUnmetAndKeepsOtherRequirements(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "高等数学", 4)
	mustCourse(t, s, "c2", "大学物理", 3)
	mustReq(t, s, "s1", "r1", "c1")
	mustReq(t, s, "s1", "r2", "c2")
	// r1：一份选课、一份未通过，没有任何通过记录。
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	mustEnroll(t, s, "s1", "r1", "2024秋", "e2")
	// r2：正常通过。
	mustEnroll(t, s, "s1", "r2", "2025春", "e3")
	if _, _, err := s.SubmitResult("s1", "e2", Failed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SubmitResult("s1", "e3", Passed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ApplyWaiver("s1", "r1", "w1", "外校同层次课程"); err != nil {
		t.Fatal(err)
	}

	// 撤销前：r1 免修 4 学分 + r2 通过 3 学分。
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 7 || len(rep.Unmet) != 0 {
		t.Fatalf("撤销前总学分应为 7 且全部满足，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	r2Before := rep.Requirements[1]
	if r2Before.Source != "enrollment" || r2Before.PassedEnrollmentID != "e3" {
		t.Fatalf("撤销前 r2 应由 e3 通过满足，得到 %+v", r2Before)
	}

	if _, changed, err := s.RevokeWaiver("s1", "w1", "依据材料不被承认"); err != nil || !changed {
		t.Fatalf("撤销应成功，changed=%v err=%v", changed, err)
	}

	rep = s.CheckStudent("s1")
	if rep.TotalCredits != 3 {
		t.Fatalf("撤销后应失去免修提供的 4 学分、只剩 r2 的 3 学分，得到 %d", rep.TotalCredits)
	}
	if len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("r1 无通过记录应重新列为未满足，得到 %v", rep.Unmet)
	}
	r1 := rep.Requirements[0]
	if r1.Satisfied || r1.Source != "" || r1.WaiverID != "" || r1.PassedEnrollmentID != "" ||
		len(r1.PassedEnrollmentIDs) != 0 {
		t.Fatalf("r1 应未满足且无任何来源（已撤销免修与未通过修读都不可用），得到 %+v", r1)
	}
	if len(rep.RevokedWaivers) != 1 || rep.RevokedWaivers[0] != "w1" {
		t.Fatalf("已撤销免修应单列，得到 %v", rep.RevokedWaivers)
	}
	// 其他要求的满足情况与学分不变。
	r2After := rep.Requirements[1]
	if r2After.Satisfied != r2Before.Satisfied || r2After.Source != r2Before.Source ||
		r2After.PassedEnrollmentID != r2Before.PassedEnrollmentID {
		t.Fatalf("撤销不应改变 r2，撤销前 %+v 撤销后 %+v", r2Before, r2After)
	}

	// 选课与未通过记录仍可查、结果原样保留。
	if e := s.Enrollment("s1", "e1"); e == nil || e.Result != Enrolled || e.Term != "2024春" {
		t.Fatalf("选课记录 e1 应仍可查且保持选课，得到 %+v", e)
	}
	if e := s.Enrollment("s1", "e2"); e == nil || e.Result != Failed || e.Term != "2024秋" {
		t.Fatalf("未通过记录 e2 应仍可查且保持未通过，得到 %+v", e)
	}
}

func TestWaiverApprovalAndRejectionHistory(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "数学", 4)
	mustReq(t, s, "s1", "r1", "c1")

	// 目标要求不存在：拒绝并入历史。
	w, a, err := s.ApplyWaiver("s1", "nope", "w0", "转学分")
	if err != nil || a != ActionCreated || w.Status != WaiverRejected || w.Reason == "" {
		t.Fatalf("不存在要求应拒绝并保留原因，得到 %+v action=%v err=%v", w, a, err)
	}
	// 依据为空：拒绝并入历史。
	w, _, err = s.ApplyWaiver("s1", "r1", "w-empty", "   ")
	if err != nil || w.Status != WaiverRejected || w.Reason == "" {
		t.Fatalf("空依据应拒绝并保留原因，得到 %+v err=%v", w, err)
	}
	if n := len(s.Waivers("s1")); n != 2 {
		t.Fatalf("被拒绝的申请应保留在免修历史中，现有 %d 条", n)
	}

	// 正常申请：有效。
	w, a, err = s.ApplyWaiver("s1", "r1", "w1", "外校同层次课程")
	if err != nil || a != ActionCreated || w.Status != WaiverApproved {
		t.Fatalf("合法免修应有效，得到 %+v action=%v err=%v", w, a, err)
	}
	// 该要求已有有效免修：再次申请被拒绝并入历史。
	w, _, err = s.ApplyWaiver("s1", "r1", "w2", "另一依据")
	if err != nil || w.Status != WaiverRejected {
		t.Fatalf("已有有效免修时应拒绝新申请，得到 %+v err=%v", w, err)
	}
	// 相同编号相同内容重复提交：返回原申请及其结果。
	if w2, a, err := s.ApplyWaiver("s1", "r1", "w2", "另一依据"); err != nil ||
		a != ActionExisted || w2.Status != WaiverRejected {
		t.Fatalf("重复提交应返回原拒绝结果，得到 %+v action=%v err=%v", w2, a, err)
	}
	// 相同编号换内容：拒绝且原记录不变。
	if _, _, err := s.ApplyWaiver("s1", "r1", "w1", "换个依据"); err == nil {
		t.Fatal("相同免修编号换内容应被拒绝")
	}
	if got := s.Waiver("s1", "w1"); got.Basis != "外校同层次课程" || got.Status != WaiverApproved {
		t.Fatalf("原免修应保持不变，得到 %+v", got)
	}
}

func TestRejectedWaiverResubmitAfterRequirementCreated(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "数学", 4)

	// 目标要求尚不存在：首次申请被拒绝，编号、目标、依据与原因都保留。
	w, a, err := s.ApplyWaiver("s1", "r1", "w1", "学科竞赛获奖")
	if err != nil || a != ActionCreated || w.Status != WaiverRejected {
		t.Fatalf("目标要求不存在应拒绝并保留申请，得到 %+v action=%v err=%v", w, a, err)
	}
	origReason := w.Reason
	if origReason == "" {
		t.Fatal("拒绝原因应非空")
	}

	// 后来补建同编号要求。
	mustReq(t, s, "s1", "r1", "c1")

	// 用原编号、原要求、原依据再次提交：返回最初的已拒绝申请，不新增、不改写。
	for i := 0; i < 2; i++ {
		w2, a, err := s.ApplyWaiver("s1", "r1", "w1", "学科竞赛获奖")
		if err != nil || a != ActionExisted {
			t.Fatalf("第 %d 次重复提交应幂等返回原申请，得到 action=%v err=%v", i+1, a, err)
		}
		if w2.Status != WaiverRejected || w2.ReqID != "r1" || w2.Basis != "学科竞赛获奖" ||
			w2.Reason != origReason {
			t.Fatalf("目标补建后重复提交不应改变原拒绝结果，得到 %+v", w2)
		}
	}
	if n := len(s.Waivers("s1")); n != 1 {
		t.Fatalf("重复提交不应新增申请，免修历史应有 1 条，得到 %d", n)
	}

	// 核对：没有通过修读也没有有效免修，要求仍未满足，总学分为零，
	// 被拒绝的旧申请不能成为学分来源。
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 0 {
		t.Fatalf("被拒绝的申请不应产生学分，总学分=%d", rep.TotalCredits)
	}
	if len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("要求 r1 应仍列为未满足，得到 %v", rep.Unmet)
	}
	if len(rep.RejectedWaivers) != 1 {
		t.Fatalf("核对应列出 1 条被拒绝的免修，得到 %d", len(rep.RejectedWaivers))
	}
	rj := rep.RejectedWaivers[0]
	if rj.Waiver.ID != "w1" || rj.Waiver.ReqID != "r1" || rj.Waiver.Basis != "学科竞赛获奖" ||
		rj.Reason != origReason {
		t.Fatalf("核对中的被拒绝记录应保持原编号、要求、依据与原因，得到 %+v", rj)
	}

	// 同编号换要求或换依据：按内容冲突拒绝，原申请与原核对结果不变。
	mustCourse(t, s, "c2", "物理", 3)
	mustReq(t, s, "s1", "r2", "c2")
	if _, _, err := s.ApplyWaiver("s1", "r2", "w1", "学科竞赛获奖"); err == nil {
		t.Fatal("已拒绝申请同编号换要求应被拒绝")
	}
	if _, _, err := s.ApplyWaiver("s1", "r1", "w1", "另一份依据"); err == nil {
		t.Fatal("已拒绝申请同编号换依据应被拒绝")
	}
	if got := s.Waiver("s1", "w1"); got.Status != WaiverRejected ||
		got.ReqID != "r1" || got.Basis != "学科竞赛获奖" || got.Reason != origReason {
		t.Fatalf("内容冲突不应改动原申请，得到 %+v", got)
	}
	if rep := s.CheckStudent("s1"); rep.TotalCredits != 0 || len(rep.RejectedWaivers) != 1 {
		t.Fatalf("内容冲突不应改变原核对结果，得到学分=%d 被拒绝=%d",
			rep.TotalCredits, len(rep.RejectedWaivers))
	}

	// 目标补建后，用新编号正常申请免修的既有行为不变。
	w3, a, err := s.ApplyWaiver("s1", "r1", "w2", "外校同层次课程")
	if err != nil || a != ActionCreated || w3.Status != WaiverApproved {
		t.Fatalf("补建要求后新编号申请应正常生效，得到 %+v action=%v err=%v", w3, a, err)
	}
	rep = s.CheckStudent("s1")
	if rep.TotalCredits != 4 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r2" {
		t.Fatalf("新免修应正常计学分，得到学分=%d 未满足=%v", rep.TotalCredits, rep.Unmet)
	}
	if len(rep.RejectedWaivers) != 1 || rep.RejectedWaivers[0].Waiver.ID != "w1" {
		t.Fatalf("旧拒绝记录应继续保留，得到 %+v", rep.RejectedWaivers)
	}
}

func TestRejectedWaiverResubmitCrossStudentRequirement(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustStudent(t, s, "s2")
	mustCourse(t, s, "c1", "数学", 4)
	// 同编号要求只在另一名学生名下存在。
	mustReq(t, s, "s2", "r1", "c1")

	// 对 s1 而言目标要求不存在：申请被拒绝。
	w, a, err := s.ApplyWaiver("s1", "r1", "w1", "证明材料")
	if err != nil || a != ActionCreated || w.Status != WaiverRejected {
		t.Fatalf("要求属于其他学生应视为不存在并拒绝，得到 %+v action=%v err=%v", w, a, err)
	}
	origReason := w.Reason
	if origReason == "" {
		t.Fatal("拒绝原因应非空")
	}

	// 后来 s1 补建自己的同编号要求，再用原内容重复提交：原拒绝结果保留。
	mustReq(t, s, "s1", "r1", "c1")
	w2, a, err := s.ApplyWaiver("s1", "r1", "w1", "证明材料")
	if err != nil || a != ActionExisted || w2.Status != WaiverRejected ||
		w2.Reason != origReason {
		t.Fatalf("补建本人要求后重复提交应保留原拒绝结果，得到 %+v action=%v err=%v", w2, a, err)
	}
	if n := len(s.Waivers("s1")); n != 1 {
		t.Fatalf("不应新增申请，s1 免修历史应有 1 条，得到 %d", n)
	}
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 0 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("旧拒绝申请不应满足要求，得到学分=%d 未满足=%v", rep.TotalCredits, rep.Unmet)
	}
	if len(rep.RejectedWaivers) != 1 || rep.RejectedWaivers[0].Reason != origReason {
		t.Fatalf("核对应保留原拒绝记录与原因，得到 %+v", rep.RejectedWaivers)
	}
	// s2 的记录不受 s1 申请影响。
	if rep2 := s.CheckStudent("s2"); len(rep2.RejectedWaivers) != 0 {
		t.Fatalf("s1 的拒绝记录不应出现在 s2 名下，得到 %+v", rep2.RejectedWaivers)
	}
}

func TestWaiverCannotCrossStudent(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustStudent(t, s, "s2")
	mustCourse(t, s, "c1", "数学", 4)
	mustReq(t, s, "s1", "r1", "c1")

	// s2 不能用 s1 的要求申请免修。
	w, _, err := s.ApplyWaiver("s2", "r1", "w1", "证明")
	if err != nil || w.Status != WaiverRejected {
		t.Fatalf("免修不能跨学生取代要求，应拒绝，得到 %+v err=%v", w, err)
	}
	if s.Requirement("s2", "r1") != nil {
		t.Fatal("被拒绝的申请不应在 s2 名下产生要求")
	}
	if rep := s.CheckStudent("s1"); len(rep.RejectedWaivers) != 0 {
		t.Fatal("s2 的拒绝历史不应出现在 s1 名下")
	}
}

func TestWaiverRevoke(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "数学", 4)
	mustReq(t, s, "s1", "r1", "c1")

	// 撤销不存在的免修应拒绝。
	if _, _, err := s.RevokeWaiver("s1", "ghost", ""); err == nil {
		t.Fatal("撤销不存在的免修应报错")
	}

	// 无通过记录时申请并撤销：要求重新列为未满足，依据保留。
	if _, _, err := s.ApplyWaiver("s1", "r1", "w1", "证明材料"); err != nil {
		t.Fatal(err)
	}
	w, changed, err := s.RevokeWaiver("s1", "w1", "材料不实")
	if err != nil || !changed || w.Status != WaiverRevoked || w.Basis != "证明材料" ||
		w.Reason != "材料不实" {
		t.Fatalf("撤销应保留依据与状态，changed=%v 得到 %+v err=%v", changed, w, err)
	}
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 0 || len(rep.Unmet) != 1 {
		t.Fatalf("撤销且无通过记录时应重新未满足，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	// 重复撤销不改变结果。
	if w2, changed, err := s.RevokeWaiver("s1", "w1", "again"); err != nil ||
		changed || w2.Reason != "材料不实" {
		t.Fatalf("重复撤销不应改变结果，changed=%v 得到 %+v err=%v", changed, w2, err)
	}
	// 已撤销的申请不能因重试重新生效。
	if w2, a, err := s.ApplyWaiver("s1", "r1", "w1", "证明材料"); err != nil ||
		a != ActionExisted || w2.Status != WaiverRevoked {
		t.Fatalf("重试已撤销申请不应使其生效，得到 %+v action=%v err=%v", w2, a, err)
	}

	// 有通过记录的要求：撤销免修后继续满足。
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
		t.Fatal(err)
	}
	rep = s.CheckStudent("s1")
	if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
		t.Fatalf("撤销免修但有通过记录时应继续满足，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	if rep.Requirements[0].Source != "enrollment" ||
		rep.Requirements[0].PassedEnrollmentID != "e1" {
		t.Fatal("撤销免修后应改以通过修读说明来源")
	}

	// 已拒绝的申请不能撤销。
	if _, _, err := s.ApplyWaiver("s1", "r1", "wbad", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RevokeWaiver("s1", "wbad", ""); err == nil {
		t.Fatal("撤销已拒绝的申请应报错")
	}
}

func TestWaiverAndPassCountOnceWithWaiverSource(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "数学", 4)
	mustReq(t, s, "s1", "r1", "c1")
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ApplyWaiver("s1", "r1", "w1", "免修依据"); err != nil {
		t.Fatal(err)
	}

	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 4 {
		t.Fatalf("通过与有效免修并存只应计一次学分，得到 %d", rep.TotalCredits)
	}
	st := rep.Requirements[0]
	if st.Source != "waiver" || st.WaiverID != "w1" {
		t.Fatalf("有有效免修时应以免修说明当前来源，得到 %+v", st)
	}
	if len(st.PassedEnrollmentIDs) != 1 || st.PassedEnrollmentIDs[0] != "e1" {
		t.Fatalf("修读历史应同时保留，得到 %v", st.PassedEnrollmentIDs)
	}
}

func TestCheckUnknownStudentAndReport(t *testing.T) {
	s := NewStore()
	rep := s.CheckStudent("nobody")
	if rep.Found {
		t.Fatal("没有记录的学生应返回不存在结果")
	}

	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "数学", 4)
	mustCourse(t, s, "c2", "物理", 3)
	mustReq(t, s, "s1", "r1", "c1")
	mustReq(t, s, "s1", "r2", "c2")
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	if _, _, err := s.SubmitResult("s1", "e1", Failed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ApplyWaiver("s1", "r2", "w1", "竞赛获奖"); err != nil {
		t.Fatal(err)
	}

	rep = s.CheckStudent("s1")
	if rep.TotalCredits != 3 {
		t.Fatalf("未通过不计学分、免修计学分，总学分应为 3，得到 %d", rep.TotalCredits)
	}
	if len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("未满足要求应为 [r1]，得到 %v", rep.Unmet)
	}
	if len(rep.Requirements) != 2 {
		t.Fatalf("应列出全部 2 项要求，得到 %d", len(rep.Requirements))
	}
	if rep.Requirements[0].Satisfied {
		t.Fatalf("r1 只有未通过，应未满足，得到 %+v", rep.Requirements[0])
	}
	if !rep.Requirements[1].Satisfied || rep.Requirements[1].Source != "waiver" ||
		rep.Requirements[1].WaiverID != "w1" {
		t.Fatalf("r2 应由有效免修满足，得到 %+v", rep.Requirements[1])
	}
}

// setupSharedEnrollments 建立两名学生就同一门 4 学分课程的同号要求 r1 与
// 同号修读 e1（同一学期），返回前、后提交结果的两名学生编号。
// 两份修读都应成功登记，初始为选课。
func setupSharedEnrollments(t *testing.T, s *Store) {
	t.Helper()
	mustStudent(t, s, "s1")
	mustStudent(t, s, "s2")
	mustCourse(t, s, "c1", "高等数学", 4)
	mustReq(t, s, "s1", "r1", "c1")
	mustReq(t, s, "s2", "r1", "c1")
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	mustEnroll(t, s, "s2", "r1", "2024春", "e1")

	e1 := s.Enrollment("s1", "e1")
	e2 := s.Enrollment("s2", "e1")
	if e1 == nil || e2 == nil || e1 == e2 {
		t.Fatalf("同号修读应在两名学生名下各成一条独立记录，得到 %p %p", e1, e2)
	}
	if e1.Result != Enrolled || e2.Result != Enrolled {
		t.Fatalf("两份修读初始都应为选课，得到 %s %s", e1.Result, e2.Result)
	}
	// 各自核对均为 0 学分且要求未满足。
	assertUnmetZero(t, s, "s1")
	assertUnmetZero(t, s, "s2")
}

// assertUnmetZero 核对该学生：0 学分、要求未满足、无任何通过修读历史。
func assertUnmetZero(t *testing.T, s *Store, student string) {
	t.Helper()
	rep := s.CheckStudent(student)
	if rep.TotalCredits != 0 {
		t.Fatalf("学生 %s 总学分应为 0，得到 %d", student, rep.TotalCredits)
	}
	if len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("学生 %s 的要求 r1 应未满足，得到 %v", student, rep.Unmet)
	}
	if st := rep.Requirements[0]; st.Satisfied || st.Source != "" ||
		st.PassedEnrollmentID != "" || len(st.PassedEnrollmentIDs) != 0 {
		t.Fatalf("学生 %s 的要求不应有来源或通过历史，得到 %+v", student, st)
	}
}

// assertPassedOwner 核对该学生的要求已满足、获得 4 学分，且来源指向本人的 e1。
func assertPassedOwner(t *testing.T, s *Store, student string) {
	t.Helper()
	rep := s.CheckStudent(student)
	if rep.TotalCredits != 4 {
		t.Fatalf("学生 %s 应只有本人获得 4 学分，得到 %d", student, rep.TotalCredits)
	}
	if len(rep.Unmet) != 0 {
		t.Fatalf("学生 %s 的要求应已满足，未满足=%v", student, rep.Unmet)
	}
	st := rep.Requirements[0]
	if !st.Satisfied || st.Source != "enrollment" || st.PassedEnrollmentID != "e1" {
		t.Fatalf("学生 %s 的要求应由本人的通过修读 e1 满足，得到 %+v", student, st)
	}
	if len(st.PassedEnrollmentIDs) != 1 || st.PassedEnrollmentIDs[0] != "e1" {
		t.Fatalf("学生 %s 的通过历史应只有本人的 e1，得到 %v",
			student, st.PassedEnrollmentIDs)
	}
	if e := s.Enrollment(student, "e1"); e == nil || e.Result != Passed {
		t.Fatalf("学生 %s 本人的修读 e1 应为通过，得到 %+v", student, e)
	}
}

// assertEnrResult 直接核对某学生名下 e1 的结果。
func assertEnrResult(t *testing.T, s *Store, student string, want Result) {
	t.Helper()
	e := s.Enrollment(student, "e1")
	if e == nil {
		t.Fatalf("学生 %s 名下应存在修读 e1", student)
	}
	if e.Result != want {
		t.Fatalf("学生 %s 的修读 e1 应为 %s，得到 %s", student, want, e.Result)
	}
}

// TestSharedEnrollmentIDsResultOwnership 两名学生共享要求编号与修读编号时，
// 提交结果只能写到本人名下；先由 s1 通过、再由 s2 未通过。
func TestSharedEnrollmentIDsResultOwnership(t *testing.T) {
	s := NewStore()
	setupSharedEnrollments(t, s)

	// 给 s1 的修读提交通过：只有 s1 获得 4 学分、要求满足、来源是本人的 e1。
	if _, changed, err := s.SubmitResult("s1", "e1", Passed); err != nil || !changed {
		t.Fatalf("s1 提交通过应成功，changed=%v err=%v", changed, err)
	}
	assertPassedOwner(t, s, "s1")
	// 另一名学生仍是选课：不能因为编号相同拿到学分或被改成通过。
	assertEnrResult(t, s, "s2", Enrolled)
	assertUnmetZero(t, s, "s2")

	// 随后给 s2 的同号修读提交未通过：s2 仍为 0 学分、要求未满足。
	if _, changed, err := s.SubmitResult("s2", "e1", Failed); err != nil || !changed {
		t.Fatalf("s2 提交未通过应成功，changed=%v err=%v", changed, err)
	}
	assertEnrResult(t, s, "s2", Failed)
	assertUnmetZero(t, s, "s2")
	// s1 原来的通过结果、学分与来源均保持不变。
	assertEnrResult(t, s, "s1", Passed)
	assertPassedOwner(t, s, "s1")

	// 两人结果不同，应分别被接受，不能被误判为同一次修读的结果冲突：
	// 各自重复提交相同结果均幂等成功。
	if _, changed, err := s.SubmitResult("s1", "e1", Passed); err != nil || changed {
		t.Fatalf("s1 重复通过应幂等，changed=%v err=%v", changed, err)
	}
	if _, changed, err := s.SubmitResult("s2", "e1", Failed); err != nil || changed {
		t.Fatalf("s2 重复未通过应幂等，changed=%v err=%v", changed, err)
	}
	assertPassedOwner(t, s, "s1")
	assertEnrResult(t, s, "s2", Failed)
	assertUnmetZero(t, s, "s2")
}

// TestSharedEnrollmentIDsResultOwnershipReversed 与上一个用例相同的归属保护，
// 但交换两名学生的登记/处理顺序：先 s2 未通过、再 s1 通过，结论必须一致，
// 证明归属不依赖先登记或先处理哪名学生。
func TestSharedEnrollmentIDsResultOwnershipReversed(t *testing.T) {
	s := NewStore()
	// 先登记 s2 再登记 s1（与 setupSharedEnrollments 顺序相反）。
	mustStudent(t, s, "s2")
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "高等数学", 4)
	mustReq(t, s, "s2", "r1", "c1")
	mustReq(t, s, "s1", "r1", "c1")
	mustEnroll(t, s, "s2", "r1", "2024春", "e1")
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	assertUnmetZero(t, s, "s1")
	assertUnmetZero(t, s, "s2")

	// 后登记的 s2 先提交未通过。
	if _, changed, err := s.SubmitResult("s2", "e1", Failed); err != nil || !changed {
		t.Fatalf("s2 提交未通过应成功，changed=%v err=%v", changed, err)
	}
	assertEnrResult(t, s, "s2", Failed)
	assertUnmetZero(t, s, "s2")
	assertEnrResult(t, s, "s1", Enrolled)
	assertUnmetZero(t, s, "s1")

	// s1 随后提交通过。
	if _, changed, err := s.SubmitResult("s1", "e1", Passed); err != nil || !changed {
		t.Fatalf("s1 提交通过应成功，changed=%v err=%v", changed, err)
	}
	assertPassedOwner(t, s, "s1")
	assertEnrResult(t, s, "s2", Failed)
	assertUnmetZero(t, s, "s2")
}

// TestSubmitResultUnknownEnrollmentUnderOtherStudent 修读编号只存在于 s1 名下时，
// 用 s2 的名义提交结果必须明确报“s2 名下没有该修读”，不能借用 s1 的修读，
// 也不能在 s2 名下补出一条新记录。
func TestSubmitResultUnknownEnrollmentUnderOtherStudent(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustStudent(t, s, "s2")
	mustCourse(t, s, "c1", "高等数学", 4)
	mustReq(t, s, "s1", "r1", "c1")
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")

	_, _, err := s.SubmitResult("s2", "e1", Passed)
	if err == nil || !strings.Contains(err.Error(), "s2") ||
		!strings.Contains(err.Error(), "e1") {
		t.Fatalf("用 s2 的名义提交只属于 s1 的修读应明确报 s2 名下无此修读，得到 %v", err)
	}
	if !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("错误信息应明确说明名下不存在，得到 %v", err)
	}

	// 不能借用 s1 的修读：s1 的 e1 仍是选课。
	assertEnrResult(t, s, "s1", Enrolled)
	// 不能为 s2 补出新记录。
	if e := s.Enrollment("s2", "e1"); e != nil {
		t.Fatalf("拒绝后不应在 s2 名下补出修读，得到 %+v", e)
	}
	if n := len(s.Enrollments("s2")); n != 0 {
		t.Fatalf("s2 不应有任何修读记录，得到 %d 条", n)
	}
	// 两人的修读状态、要求满足情况和总学分都与提交前一致。
	assertUnmetZero(t, s, "s1")
	if rep := s.CheckStudent("s2"); rep.TotalCredits != 0 ||
		len(rep.Requirements) != 0 || len(rep.Unmet) != 0 {
		t.Fatalf("s2 本就没有要求，拒绝后核对结果不应变化，得到 %+v", rep)
	}
}

// TestSharedEnrollmentResultChangeRejectedKeepsBoth 一名学生已提交结果后，
// 再把本人这份修读改成另一结果仍按现有规则拒绝并保留原结果；
// 另一名学生的同号修读保持原样。
func TestSharedEnrollmentResultChangeRejectedKeepsBoth(t *testing.T) {
	s := NewStore()
	setupSharedEnrollments(t, s)

	if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SubmitResult("s2", "e1", Failed); err != nil {
		t.Fatal(err)
	}

	// s1 已通过，改成未通过：拒绝并保留通过。
	if _, _, err := s.SubmitResult("s1", "e1", Failed); err == nil {
		t.Fatal("s1 通过后改提未通过应被拒绝")
	}
	assertEnrResult(t, s, "s1", Passed)
	assertPassedOwner(t, s, "s1")
	// s2 的同号修读保持未通过、0 学分。
	assertEnrResult(t, s, "s2", Failed)
	assertUnmetZero(t, s, "s2")

	// s2 已未通过，改成通过：拒绝并保留未通过；s1 不受影响。
	if _, _, err := s.SubmitResult("s2", "e1", Passed); err == nil {
		t.Fatal("s2 未通过后改提通过应被拒绝")
	}
	assertEnrResult(t, s, "s2", Failed)
	assertUnmetZero(t, s, "s2")
	assertEnrResult(t, s, "s1", Passed)
	assertPassedOwner(t, s, "s1")
}
