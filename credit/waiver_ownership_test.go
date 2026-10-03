package credit

import (
	"path/filepath"
	"strings"
	"testing"
)

// 免修编号只在所属学生名下唯一：本组用例为两名学生共享要求编号 r1 与
// 免修编号 w1 的情形提供可重复执行的回归检查，覆盖申请归属、有效状态、
// 学分来源与免修历史的隔离，以及同一学生名下既有冲突规则不受影响。

// sharedWaiverOwner 描述一名参与“同号要求 + 同号免修”情形的学生。
type sharedWaiverOwner struct {
	student string
	course  string
	name    string
	credit  int
	basis   string
}

func sharedWaiverOwners() []sharedWaiverOwner {
	return []sharedWaiverOwner{
		{"s1", "c1", "高等数学", 4, "学科竞赛获奖"},
		{"s2", "c2", "线性代数", 3, "外校修读证明"},
	}
}

// setupSharedWaivers 按 registerOrder 的顺序登记两名学生：各自有一项编号为
// r1 的要求，分别指向 4 学分和 3 学分的课程，两人都没有任何修读。
func setupSharedWaivers(t *testing.T, s *Store, registerOrder []sharedWaiverOwner) {
	t.Helper()
	for _, o := range sharedWaiverOwners() {
		mustCourse(t, s, o.course, o.name, o.credit)
	}
	for _, o := range registerOrder {
		mustStudent(t, s, o.student)
	}
	for _, o := range registerOrder {
		mustReq(t, s, o.student, "r1", o.course)
	}
	// 两人初始都没有通过修读：0 学分、要求未满足、没有任何免修历史。
	for _, o := range registerOrder {
		assertWaiverOwnerUnmet(t, s, o.student, o)
	}
}

// assertWaiverOwnerUnmet 核对该学生尚未因同号免修获得任何东西：
// 0 学分、r1 未满足且无来源，免修历史中没有任何人的申请。
func assertWaiverOwnerUnmet(t *testing.T, s *Store, student string, o sharedWaiverOwner) {
	t.Helper()
	rep := s.CheckStudent(student)
	if rep.TotalCredits != 0 {
		t.Fatalf("学生 %s 在申请前总学分应为 0，得到 %d", student, rep.TotalCredits)
	}
	if len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("学生 %s 的要求 r1 应未满足，得到 %v", student, rep.Unmet)
	}
	if len(rep.Requirements) != 1 {
		t.Fatalf("学生 %s 应只有一项要求 r1，得到 %d 项", student, len(rep.Requirements))
	}
	st := rep.Requirements[0]
	if st.Satisfied || st.Source != "" || st.WaiverID != "" {
		t.Fatalf("学生 %s 的要求不应有免修来源，得到 %+v", student, st)
	}
	if st.Course == nil || st.Course.ID != o.course || st.Course.Credit != o.credit {
		t.Fatalf("学生 %s 的 r1 应指向本人课程 %s（%d 学分），得到 %+v",
			student, o.course, o.credit, st.Course)
	}
	if w := s.Waiver(student, "w1"); w != nil {
		t.Fatalf("学生 %s 名下在申请前不应存在免修 w1，得到 %+v", student, w)
	}
	if ws := s.Waivers(student); len(ws) != 0 {
		t.Fatalf("学生 %s 的免修历史应为空，得到 %+v", student, ws)
	}
	if len(rep.RejectedWaivers) != 0 || len(rep.RevokedWaivers) != 0 {
		t.Fatalf("学生 %s 不应有被拒绝或已撤销的免修，得到 %+v / %v",
			student, rep.RejectedWaivers, rep.RevokedWaivers)
	}
}

// assertWaiverOwnerSatisfied 核对该学生的 r1 由本人名下的 w1 满足：
// 学分是本人课程的学分、来源指向本人的 w1、免修历史只保留本人依据。
func assertWaiverOwnerSatisfied(t *testing.T, s *Store, student string, o sharedWaiverOwner) {
	t.Helper()
	rep := s.CheckStudent(student)
	if rep.TotalCredits != o.credit {
		t.Fatalf("学生 %s 应只有本人课程的 %d 学分，得到 %d",
			student, o.credit, rep.TotalCredits)
	}
	if len(rep.Unmet) != 0 {
		t.Fatalf("学生 %s 的要求应已满足，未满足=%v", student, rep.Unmet)
	}
	st := rep.Requirements[0]
	if !st.Satisfied || st.Source != "waiver" || st.WaiverID != "w1" {
		t.Fatalf("学生 %s 的要求应由本人的有效免修 w1 满足，得到 %+v", student, st)
	}
	if st.Course == nil || st.Course.ID != o.course || st.Course.Credit != o.credit {
		t.Fatalf("学生 %s 的学分来源应是本人课程 %s《%s》%d 学分，得到 %+v",
			student, o.course, o.name, o.credit, st.Course)
	}
	if len(rep.RejectedWaivers) != 0 || len(rep.RevokedWaivers) != 0 {
		t.Fatalf("学生 %s 不应有被拒绝或已撤销的免修，得到 %+v / %v",
			student, rep.RejectedWaivers, rep.RevokedWaivers)
	}
	w := s.Waiver(student, "w1")
	if w == nil {
		t.Fatalf("学生 %s 名下应存在免修 w1", student)
	}
	if w.StudentID != student || w.ReqID != "r1" || w.Basis != o.basis ||
		w.Status != WaiverApproved {
		t.Fatalf("学生 %s 的 w1 应是本人 r1 的有效免修、依据 %q，得到 %+v",
			student, o.basis, w)
	}
	ws := s.Waivers(student)
	if len(ws) != 1 || ws[0] != w {
		t.Fatalf("学生 %s 的免修历史应只有本人的 w1 一条，得到 %+v", student, ws)
	}
}

// runSharedWaiverOwnershipScenario 是两种提交顺序共用的检查主体：
// 先由 first 提交 w1，确认只影响 first；再由另一人提交同号 w1，
// 确认仍正常生效且两人的归属互不干扰。
func runSharedWaiverOwnershipScenario(t *testing.T, s *Store, first string) {
	t.Helper()
	owners := map[string]sharedWaiverOwner{}
	for _, o := range sharedWaiverOwners() {
		owners[o.student] = o
	}
	second := "s2"
	if first == "s2" {
		second = "s1"
	}
	of, os2 := owners[first], owners[second]

	// 先提交的一份生效后，只能使申请人的要求满足。
	wFirst, a, err := s.ApplyWaiver(first, "r1", "w1", of.basis)
	if err != nil || a != ActionCreated || wFirst.Status != WaiverApproved {
		t.Fatalf("%s 首次提交 w1 应正常生效，得到 %+v action=%v err=%v",
			first, wFirst, a, err)
	}
	if wFirst.StudentID != first || wFirst.ReqID != "r1" || wFirst.Basis != of.basis {
		t.Fatalf("%s 的 w1 应登记在本人名下、指向本人 r1 与本人依据，得到 %+v",
			first, wFirst)
	}
	assertWaiverOwnerSatisfied(t, s, first, of)

	// 另一人的要求仍未满足、学分为零，免修历史中不能出现前一人的申请。
	assertWaiverOwnerUnmet(t, s, second, os2)
	if ws := s.Waivers(second); len(ws) != 0 {
		t.Fatalf("%s 的免修历史不能出现 %s 的申请，得到 %+v", second, first, ws)
	}

	// 另一人随后提交自己的同号申请：不能被当作重复提交、编号冲突，
	// 也不能误判为该要求已经免修，必须作为本人的新申请正常生效。
	wSecond, a, err := s.ApplyWaiver(second, "r1", "w1", os2.basis)
	if err != nil || a != ActionCreated || wSecond.Status != WaiverApproved {
		t.Fatalf("%s 提交同号 w1 应正常生效而非被判重复/冲突，得到 %+v action=%v err=%v",
			second, wSecond, a, err)
	}
	if wFirst == wSecond {
		t.Fatal("两名学生的同号免修应是各自独立的记录，不应返回同一条")
	}

	// 此时两人的核对结果分别为各自课程学分，来源均指向本人名下的 w1，
	// 免修历史各保留本人的依据。
	assertWaiverOwnerSatisfied(t, s, first, of)
	assertWaiverOwnerSatisfied(t, s, second, os2)

	// 要求和免修编号相同，仍应能从各自的学生记录中分清对应的课程与依据。
	if s.Requirement(first, "r1") == s.Requirement(second, "r1") {
		t.Fatal("两名学生的同号要求应是各自独立的记录")
	}
	if got := s.Requirement(first, "r1"); got.CourseID != of.course {
		t.Fatalf("%s 的 r1 应指向 %s，得到 %s", first, of.course, got.CourseID)
	}
	if got := s.Requirement(second, "r1"); got.CourseID != os2.course {
		t.Fatalf("%s 的 r1 应指向 %s，得到 %s", second, os2.course, got.CourseID)
	}
}

// TestSharedWaiverIDsOwnership 两名学生的要求编号与免修编号完全相同
// （r1/w1），s1 先提交：申请归属、有效状态、学分来源与免修历史必须各归各。
func TestSharedWaiverIDsOwnership(t *testing.T) {
	s := NewStore()
	owners := sharedWaiverOwners()
	setupSharedWaivers(t, s, owners)
	runSharedWaiverOwnershipScenario(t, s, "s1")

	// 保存并重新打开：同号记录的归属、来源与历史仍应能按学生分清。
	path := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("Load: existed=%v err=%v", existed, err)
	}
	for _, o := range owners {
		assertWaiverOwnerSatisfied(t, loaded, o.student, o)
	}
}

// TestSharedWaiverIDsOwnershipReversed 交换登记与提交顺序（s2 先登记、
// 先提交），归属结论必须一致，证明规则不依赖先登记或先处理哪名学生。
func TestSharedWaiverIDsOwnershipReversed(t *testing.T) {
	s := NewStore()
	owners := sharedWaiverOwners()
	reverse := []sharedWaiverOwner{owners[1], owners[0]}
	setupSharedWaivers(t, s, reverse)
	runSharedWaiverOwnershipScenario(t, s, "s2")
}

// TestSharedWaiverSameStudentRulesDoNotCross 在两名学生各有一份同号 w1
// 生效后，同一学生名下既有的幂等与冲突限制仍要成立，且任何拒绝都不能
// 改动另一人的同号记录。
func TestSharedWaiverSameStudentRulesDoNotCross(t *testing.T) {
	s := NewStore()
	owners := sharedWaiverOwners()
	setupSharedWaivers(t, s, owners)

	s1, s2 := owners[0], owners[1]
	w1s1, a, err := s.ApplyWaiver("s1", "r1", "w1", s1.basis)
	if err != nil || a != ActionCreated || w1s1.Status != WaiverApproved {
		t.Fatalf("s1 首次申请应生效，得到 %+v action=%v err=%v", w1s1, a, err)
	}
	w1s2, a, err := s.ApplyWaiver("s2", "r1", "w1", s2.basis)
	if err != nil || a != ActionCreated || w1s2.Status != WaiverApproved {
		t.Fatalf("s2 同号申请应生效，得到 %+v action=%v err=%v", w1s2, a, err)
	}

	// 原编号、原要求、原依据再次提交：返回本人的原申请，历史条数和学分不增加。
	for i := 0; i < 2; i++ {
		got, a, err := s.ApplyWaiver("s1", "r1", "w1", s1.basis)
		if err != nil || a != ActionExisted || got != w1s1 {
			t.Fatalf("第 %d 次原样重复提交应返回 s1 的原申请，得到 %+v action=%v err=%v",
				i+1, got, a, err)
		}
		if got.Status != WaiverApproved || got.Basis != s1.basis {
			t.Fatalf("重复提交不应改变原申请，得到 %+v", got)
		}
	}
	if n := len(s.Waivers("s1")); n != 1 {
		t.Fatalf("原样重复提交不应新增申请，s1 历史应有 1 条，得到 %d", n)
	}
	assertWaiverOwnerSatisfied(t, s, "s1", s1)

	// 在原 w1 下改换依据：按内容冲突拒绝，不新增申请，原依据与有效状态保留。
	if _, _, err := s.ApplyWaiver("s1", "r1", "w1", "改换后的依据"); err == nil {
		t.Fatal("同一学生在原免修编号下改换依据应被拒绝")
	} else if !strings.Contains(err.Error(), "w1") {
		t.Fatalf("冲突拒绝应点名原免修编号 w1，得到 %v", err)
	}
	if n := len(s.Waivers("s1")); n != 1 {
		t.Fatalf("内容冲突不应新增申请，s1 历史应仍为 1 条，得到 %d", n)
	}
	if got := s.Waiver("s1", "w1"); got != w1s1 || got.Status != WaiverApproved ||
		got.Basis != s1.basis || got.ReqID != "r1" {
		t.Fatalf("冲突拒绝后 s1 的原 w1 应保持有效且依据不变，得到 %+v", got)
	}
	if rep := s.CheckStudent("s1"); rep.TotalCredits != 4 || len(rep.RejectedWaivers) != 0 {
		t.Fatalf("冲突拒绝不应改变 s1 的学分或增加拒绝历史，得到学分=%d 拒绝=%d",
			rep.TotalCredits, len(rep.RejectedWaivers))
	}
	// 不能改动另一人的同号记录。
	if got := s.Waiver("s2", "w1"); got != w1s2 || got.Status != WaiverApproved ||
		got.Basis != s2.basis {
		t.Fatalf("s1 的冲突拒绝不应影响 s2 的 w1，得到 %+v", got)
	}
	assertWaiverOwnerSatisfied(t, s, "s2", s2)

	// 改用新免修编号 w2 再次取代已经免修的 r1：应被拒绝，并在提交者历史中
	// 留下一条独立的拒绝记录（与 w1 并存，编号、目标、依据各不相同）。
	w2, a, err := s.ApplyWaiver("s1", "r1", "w2", "再次申请的新依据")
	if err != nil || a != ActionCreated || w2.Status != WaiverRejected {
		t.Fatalf("新编号取代已有免修应新建被拒绝记录，得到 %+v action=%v err=%v",
			w2, a, err)
	}
	if w2.ID != "w2" || w2.ReqID != "r1" || w2.Basis != "再次申请的新依据" ||
		w2.Reason == "" || !strings.Contains(w2.Reason, "w1") {
		t.Fatalf("拒绝记录应保留编号 w2、目标 r1、新依据，并指明已有有效免修 w1，得到 %+v", w2)
	}
	if ws := s.Waivers("s1"); len(ws) != 2 || ws[0] != w1s1 || ws[1] != w2 {
		t.Fatalf("s1 历史应为 [w1 有效, w2 已拒绝] 两条独立记录，得到 %+v", ws)
	}

	// 该学生的核对结果必须列出这次申请的编号、目标要求、依据，以及该要求
	// 已有哪份有效免修的具体原因，不能只显示一个没有对应申请的失败提示。
	rep := s.CheckStudent("s1")
	if len(rep.RejectedWaivers) != 1 {
		t.Fatalf("核对应列出 1 条被拒绝的免修申请，得到 %d", len(rep.RejectedWaivers))
	}
	rj := rep.RejectedWaivers[0]
	if rj.Waiver != w2 || rj.Waiver.ID != "w2" || rj.Waiver.ReqID != "r1" ||
		rj.Waiver.Basis != "再次申请的新依据" || !strings.Contains(rj.Reason, "w1") {
		t.Fatalf("核对中的拒绝记录应对应 w2 申请并点名已有有效免修 w1，得到 %+v", rj)
	}
	out := rep.String()
	for _, want := range []string{
		"免修 w2（要求 r1，依据 \"再次申请的新依据\"）",
		"该要求已有有效免修 w1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("核对文本应包含 %q，out=%q", want, out)
		}
	}

	// 原有效申请继续作为学分来源，课程学分仍只计一次，要求仍为已满足。
	if rep.TotalCredits != 4 {
		t.Fatalf("拒绝重复取代后仍应只计一次 4 学分，得到 %d", rep.TotalCredits)
	}
	if len(rep.Unmet) != 0 {
		t.Fatalf("r1 应仍为已满足，未满足=%v", rep.Unmet)
	}
	st := rep.Requirements[0]
	if !st.Satisfied || st.Source != "waiver" || st.WaiverID != "w1" {
		t.Fatalf("学分来源应仍是原有效免修 w1，得到 %+v", st)
	}
	if got := s.Waiver("s1", "w1"); got.Status != WaiverApproved || got.Basis != s1.basis {
		t.Fatalf("原 w1 应继续有效且依据不变，得到 %+v", got)
	}
	// 被拒绝的 w2 原样重复提交同样不新增、不生效。
	if got, a, err := s.ApplyWaiver("s1", "r1", "w2", "再次申请的新依据"); err != nil ||
		a != ActionExisted || got != w2 || got.Status != WaiverRejected {
		t.Fatalf("w2 重复提交应返回原拒绝记录，得到 %+v action=%v err=%v", got, a, err)
	}
	if n := len(s.Waivers("s1")); n != 2 {
		t.Fatalf("w2 重复提交不应新增记录，s1 历史应仍为 2 条，得到 %d", n)
	}

	// 另一人的免修历史、有效申请和核对结果始终保持原样。
	if ws := s.Waivers("s2"); len(ws) != 1 || ws[0] != w1s2 {
		t.Fatalf("s2 的免修历史应始终只有本人的 w1，得到 %+v", ws)
	}
	assertWaiverOwnerSatisfied(t, s, "s2", s2)
}
