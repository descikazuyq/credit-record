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

// TestRejectedWaiverStaysRejectedAfterRequirementBuilt 回归保障：一份因目标要求
// 不存在而被拒绝的免修申请，在该要求后来补建后，按原编号、原要求、原依据重复
// 提交时仍返回最初那条已拒绝申请——不会变成有效免修、不新增申请、不丢失或改写
// 原拒绝原因；核对结果与历史查询同样保持。
func TestRejectedWaiverStaysRejectedAfterRequirementBuilt(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "数学", 4)

	// 首次申请：s1 名下尚无 r1，带非空依据，应被拒绝且原因具体。
	const basis = "学科竞赛获奖"
	w, a, err := s.ApplyWaiver("s1", "r1", "w1", basis)
	if err != nil || a != ActionCreated || w.Status != WaiverRejected {
		t.Fatalf("目标要求不存在时应拒绝并入历史，得到 %+v action=%v err=%v", w, a, err)
	}
	if w.ReqID != "r1" || w.Basis != basis || !strings.Contains(w.Reason, "不存在") {
		t.Fatalf("申请编号、目标要求、依据和具体拒绝原因都应保留，得到 %+v", w)
	}
	firstReason := w.Reason
	if n := len(s.Waivers("s1")); n != 1 {
		t.Fatalf("首次申请后应只有 1 条免修历史，得到 %d", n)
	}

	// 后来为该学生补建同编号的课程要求。
	mustReq(t, s, "s1", "r1", "c1")

	// 再用原免修编号、原要求、原依据提交：返回最初的已拒绝申请。
	w2, a2, err := s.ApplyWaiver("s1", "r1", "w1", basis)
	if err != nil || a2 != ActionExisted || w2 != w {
		t.Fatalf("重复提交应幂等返回原记录，得到 %+v action=%v err=%v，原记录=%p",
			w2, a2, err, w)
	}
	if w2.Status != WaiverRejected || w2.Reason != firstReason {
		t.Fatalf("重复提交不能把旧申请改成有效或改写拒绝原因，得到 %+v", w2)
	}
	if n := len(s.Waivers("s1")); n != 1 {
		t.Fatalf("重复提交不应新增申请，免修历史应仍为 1 条，得到 %d", n)
	}

	// 核对：无通过修读、无其他有效免修，要求仍未满足、总学分为零，
	// 旧申请不能成为学分来源；被拒绝记录与核对对应。
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 0 {
		t.Fatalf("旧拒绝申请不能带来学分，总学分应为 0，得到 %d", rep.TotalCredits)
	}
	if len(rep.Requirements) != 1 || rep.Requirements[0].Satisfied ||
		rep.Requirements[0].Source != "" {
		t.Fatalf("补建要求但无修读无有效免修时应未满足，得到 %+v", rep.Requirements)
	}
	if len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("r1 应列为未满足，得到 %v", rep.Unmet)
	}
	if len(rep.RejectedWaivers) != 1 {
		t.Fatalf("核对中应保留 1 条被拒绝免修，得到 %d", len(rep.RejectedWaivers))
	}
	rj := rep.RejectedWaivers[0]
	if rj.Waiver.ID != "w1" || rj.Waiver.ReqID != "r1" ||
		rj.Waiver.Basis != basis || rj.Reason != firstReason {
		t.Fatalf("核对中的拒绝记录应展示同一编号/要求/依据/原因，得到 %+v", rj)
	}

	// 历史查询：同一个免修编号、目标要求、依据和具体原因。
	got := s.Waiver("s1", "w1")
	if got != w || got.Status != WaiverRejected || got.Reason != firstReason {
		t.Fatalf("历史查询应返回同一条已拒绝记录及原因，得到 %+v", got)
	}

	// 同编号换成另一项要求：内容冲突，明确拒绝，原申请与原核对结果保留。
	mustCourse(t, s, "c2", "物理", 3)
	mustReq(t, s, "s1", "r2", "c2")
	if _, _, err := s.ApplyWaiver("s1", "r2", "w1", basis); err == nil {
		t.Fatal("同编号换另一项要求应按内容冲突拒绝")
	}
	// 同编号换另一份依据：同样拒绝。
	if _, _, err := s.ApplyWaiver("s1", "r1", "w1", "另一份依据"); err == nil {
		t.Fatal("同编号换另一份依据应按内容冲突拒绝")
	}
	got = s.Waiver("s1", "w1")
	if got != w || got.Status != WaiverRejected || got.ReqID != "r1" ||
		got.Basis != basis || got.Reason != firstReason {
		t.Fatalf("冲突提交后原申请应保持不变，得到 %+v", got)
	}
	if n := len(s.Waivers("s1")); n != 1 {
		t.Fatalf("冲突提交不应新增申请，得到 %d 条", n)
	}
	rep = s.CheckStudent("s1")
	if rep.TotalCredits != 0 || len(rep.Unmet) != 2 || len(rep.RejectedWaivers) != 1 {
		t.Fatalf("冲突提交不应改变核对结果，得到学分=%d 未满足=%v 拒绝=%d 条",
			rep.TotalCredits, rep.Unmet, len(rep.RejectedWaivers))
	}

	// 补建要求后，用新编号就该要求正常申请新免修的既有行为不受影响。
	w3, a3, err := s.ApplyWaiver("s1", "r1", "w3", "新的免修依据")
	if err != nil || a3 != ActionCreated || w3.Status != WaiverApproved {
		t.Fatalf("补建后新编号正常申请应仍然有效，得到 %+v action=%v err=%v", w3, a3, err)
	}
	rep = s.CheckStudent("s1")
	if rep.TotalCredits != 4 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r2" {
		t.Fatalf("新免修应正常带来学分而旧拒绝记录保留，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	if len(rep.RejectedWaivers) != 1 || rep.RejectedWaivers[0].Waiver.ID != "w1" {
		t.Fatalf("旧拒绝记录应继续列在核对中，得到 %+v", rep.RejectedWaivers)
	}
}

// TestRejectedWaiverStaysRejectedWhenReqExistedUnderOtherStudent 覆盖跨学生情形：
// 同编号课程要求在另一名学生名下已存在，仍属于申请者名下没有该要求；后来补建
// 自己的同编号要求，原拒绝结果同样保留。
func TestRejectedWaiverStaysRejectedWhenReqExistedUnderOtherStudent(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustStudent(t, s, "s2")
	mustCourse(t, s, "c1", "数学", 4)
	// r1 只在 s2 名下存在；s1 名下没有。
	mustReq(t, s, "s2", "r1", "c1")

	const basis = "竞赛获奖证明"
	w, a, err := s.ApplyWaiver("s1", "r1", "w1", basis)
	if err != nil || a != ActionCreated || w.Status != WaiverRejected ||
		!strings.Contains(w.Reason, "不存在") {
		t.Fatalf("要求只在他人名下时对申请者等同不存在，应拒绝，得到 %+v action=%v err=%v",
			w, a, err)
	}
	firstReason := w.Reason
	if n := len(s.Waivers("s1")); n != 1 || len(s.Waivers("s2")) != 0 {
		t.Fatalf("拒绝记录只应落在申请者 s1 名下，s1=%d s2=%d",
			len(s.Waivers("s1")), len(s.Waivers("s2")))
	}

	// 后来补建 s1 自己的同编号要求，再按原内容重复提交。
	mustReq(t, s, "s1", "r1", "c1")
	w2, a2, err := s.ApplyWaiver("s1", "r1", "w1", basis)
	if err != nil || a2 != ActionExisted || w2 != w ||
		w2.Status != WaiverRejected || w2.Reason != firstReason {
		t.Fatalf("补建自己的要求后重复提交仍应返回原拒绝记录，得到 %+v action=%v err=%v",
			w2, a2, err)
	}
	if n := len(s.Waivers("s1")); n != 1 {
		t.Fatalf("不应新增申请，得到 %d 条", n)
	}

	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 0 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("s1 无通过无有效免修时 r1 应未满足、学分为 0，得到 %+v", rep)
	}
	if len(rep.RejectedWaivers) != 1 ||
		rep.RejectedWaivers[0].Waiver.ID != "w1" ||
		rep.RejectedWaivers[0].Waiver.ReqID != "r1" ||
		rep.RejectedWaivers[0].Waiver.Basis != basis ||
		rep.RejectedWaivers[0].Reason != firstReason {
		t.Fatalf("核对中的拒绝记录应保持同一编号/要求/依据/原因，得到 %+v",
			rep.RejectedWaivers)
	}
	// s2 名下要求按自身情况核对，不受 s1 的拒绝历史影响。
	if rep2 := s.CheckStudent("s2"); len(rep2.RejectedWaivers) != 0 {
		t.Fatalf("s2 核对中不应出现 s1 的拒绝记录，得到 %+v", rep2.RejectedWaivers)
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
