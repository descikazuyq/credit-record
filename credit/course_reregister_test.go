package credit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件围绕“用 course 重新登记已有课程编号”的行为做回归保障：
// 课程名称、学分与开放状态会影响课程查询（Courses/Course）以及学生核对
// （CheckStudent/show 行），因此检查同时覆盖操作后的课程资料与核对结果。

// TestCourseReregisterUnreferencedUpdatesInPlace 课程尚未被任何学生的课程
// 要求引用时，用原编号提交新名称和新的正整数学分，应原地更新原课程：
// 列表里只保留这一份记录，并显示新名称与新学分。
func TestCourseReregisterUnreferencedUpdatesInPlace(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "数学", 4)

	c, a, err := s.AddCourse("c1", "高等数学", 5)
	if err != nil || a != ActionUpdated {
		t.Fatalf("未被引用的课程用原编号提交新名称、新学分应更新，得到 action=%v err=%v", a, err)
	}
	if c.ID != "c1" || c.Name != "高等数学" || c.Credit != 5 {
		t.Fatalf("更新后应为新名称与新学分，得到 %+v", c)
	}
	if !c.Open {
		t.Fatalf("原来开放的课程更新后应继续开放，得到 %+v", c)
	}

	// 课程列表只保留这一份记录，且显示新名称与新学分。
	all := s.Courses()
	if len(all) != 1 || all[0] != c {
		t.Fatalf("课程列表应只保留原地更新的同一份记录，得到 %d 条（同指针=%v）",
			len(all), len(all) == 1 && all[0] == c)
	}
	if all[0].Name != "高等数学" || all[0].Credit != 5 {
		t.Fatalf("列表中应显示新名称与新学分，得到 %+v", all[0])
	}
}

// TestCourseReregisterClosedUnreferencedStaysClosed 已停开的课程在未被
// 引用时同样允许更新，但重新登记不能让它恢复开放：更新后仍为停开。
func TestCourseReregisterClosedUnreferencedStaysClosed(t *testing.T) {
	s := NewStore()
	mustCourse(t, s, "c1", "数学", 4)
	if _, err := s.SetCourseOpen("c1", false); err != nil {
		t.Fatal(err)
	}

	c, a, err := s.AddCourse("c1", "高等数学", 5)
	if err != nil || a != ActionUpdated {
		t.Fatalf("停开但未被引用的课程也应允许更新，得到 action=%v err=%v", a, err)
	}
	if c.Open {
		t.Fatalf("停开课程重新登记后必须仍为停开，不能恢复开放，得到 %+v", c)
	}
	if c.Name != "高等数学" || c.Credit != 5 {
		t.Fatalf("停开课程也应更新为新名称、新学分，得到 %+v", c)
	}
	if n := len(s.Courses()); n != 1 {
		t.Fatalf("更新后仍应只有一份课程记录，得到 %d 条", n)
	}
}

// TestCourseReregisterFeedsLaterRequirement 未引用期间更新资料后，后续
// 学生对该课程建立的要求、修读与核对都应使用更新后的资料（新名称、新学分）。
func TestCourseReregisterFeedsLaterRequirement(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "旧名称", 4)
	if _, a, err := s.AddCourse("c1", "高等数学", 5); err != nil || a != ActionUpdated {
		t.Fatalf("前置更新失败：action=%v err=%v", a, err)
	}

	// 更新后才建立要求：要求指向同一课程编号。
	mustReq(t, s, "s1", "r1", "c1")
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
		t.Fatal(err)
	}

	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 5 {
		t.Fatalf("后续要求应使用新学分 5，总学分=%d", rep.TotalCredits)
	}
	st := rep.Requirements[0]
	if st.Course == nil || st.Course.Name != "高等数学" || st.Course.Credit != 5 {
		t.Fatalf("核对结果应携带更新后的课程资料，得到 %+v", st.Course)
	}
}

// reloadFresh 保存当前记录并重新加载，返回一个“自加载以来没有任何变更”
// （Dirty()==false）的存储，用于精确判断某次被拒绝的操作本身是否标记落盘。
func reloadFresh(t *testing.T, s *Store) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("Load: existed=%v err=%v", existed, err)
	}
	return loaded
}

// TestCourseReregisterReferencedCreditRejectedAtomically 一旦任意学生的
// 课程要求引用了该课程，同时提交新名称和不同学分必须整次拒绝：不能出现
// 学分没改但名称已经改掉的情况，开放状态也保持原样。
func TestCourseReregisterReferencedCreditRejectedAtomically(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "数学", 4)
	mustReq(t, s, "s1", "r1", "c1") // 只建立要求，不选课、不产生结果。
	s = reloadFresh(t, s)           // 在干净记录上检验拒绝本身不落盘。

	_, a, err := s.AddCourse("c1", "高等数学", 5)
	if err == nil || a != "" {
		t.Fatalf("已被要求引用的课程改学分应被拒绝，得到 action=%v err=%v", a, err)
	}
	// 错误说明应指出课程已被要求引用以及原学分。
	msg := err.Error()
	if !strings.Contains(msg, "引用") || !strings.Contains(msg, "原学分 4") {
		t.Fatalf("错误说明应指出已被要求引用及原学分 4，得到 %q", msg)
	}

	// 原名称、原学分和开放状态全部保留。
	c := s.Course("c1")
	if c.Name != "数学" || c.Credit != 4 || !c.Open {
		t.Fatalf("拒绝后课程应保持原名称、原学分、原开放状态，得到 %+v", c)
	}
	if s.Dirty() {
		t.Fatal("被拒绝的更新不应标记为已变更，不应落盘")
	}
	if n := len(s.Courses()); n != 1 {
		t.Fatalf("拒绝后课程记录数应不变，得到 %d 条", n)
	}

	// 以原名称、原学分重新提交属于内容一致的幂等命中，仍返回原记录。
	if c2, a2, err := s.AddCourse("c1", "数学", 4); err != nil || a2 != ActionExisted || c2 != c {
		t.Fatalf("原内容重复提交应幂等返回原记录，得到 action=%v err=%v %p", a2, err, c2)
	}
}

// TestCourseReregisterReferencedGuardActiveWithoutEnrollment 限制从建立
// 要求时即生效：不需要先有选课或通过结果；课程停开也不会解除限制。
func TestCourseReregisterReferencedGuardActiveWithoutEnrollment(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "数学", 4)
	mustReq(t, s, "s1", "r1", "c1")
	// 没有任何 enroll/pass/fail；随后停开课程。
	if _, err := s.SetCourseOpen("c1", false); err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.AddCourse("c1", "高等数学", 6); err == nil {
		t.Fatal("仅建立要求（无选课）且课程已停开时，改学分仍应被拒绝")
	}
	c := s.Course("c1")
	if c.Name != "数学" || c.Credit != 4 || c.Open {
		t.Fatalf("停开状态下拒绝改学分也应完整保留原记录，得到 %+v", c)
	}
}

// TestCourseReregisterReferencedRenameSameCreditAllowed 已被引用的课程
// 仍允许只改名称，前提是学分与原值相同：编号与学分保持原样，已有学生的
// 要求查询显示新名称；修读、免修归属、核对的满足情况、学分来源和总学分
// 都不因改名改变。
func TestCourseReregisterReferencedRenameSameCreditAllowed(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "旧名称", 4)
	mustReq(t, s, "s1", "r1", "c1")
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ApplyWaiver("s1", "r1", "w1", "学科竞赛获奖"); err != nil {
		t.Fatal(err)
	}

	c, a, err := s.AddCourse("c1", "高等数学", 4) // 新名称、同学分。
	if err != nil || a != ActionUpdated {
		t.Fatalf("已引用课程提交相同学分应允许只改名称，得到 action=%v err=%v", a, err)
	}
	if c.ID != "c1" || c.Credit != 4 || !c.Open {
		t.Fatalf("改名后课程编号、学分与开放状态应不变，得到 %+v", c)
	}
	if c.Name != "高等数学" {
		t.Fatalf("名称应更新为新名称，得到 %+v", c)
	}
	if n := len(s.Courses()); n != 1 || s.Courses()[0] != c {
		t.Fatalf("改名应原地更新，课程仍只有一份，得到 %d 条", n)
	}

	// 已有要求仍指向同一编号，课程资料呈现新名称。
	r := s.Requirement("s1", "r1")
	if r == nil || r.CourseID != "c1" {
		t.Fatalf("改名不应改动课程要求指向，得到 %+v", r)
	}

	// 核对：满足情况、学分来源（有效免修优先）与总学分（一份 4 学分）不变，
	// 通过修读历史仍指向原要求上的 e1。
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 4 {
		t.Fatalf("改名不应改变总学分，得到 %d", rep.TotalCredits)
	}
	if len(rep.Unmet) != 0 {
		t.Fatalf("改名不应让已满足要求变成未满足，得到 %v", rep.Unmet)
	}
	st := rep.Requirements[0]
	if !st.Satisfied || st.Source != "waiver" || st.WaiverID != "w1" {
		t.Fatalf("改名后仍应由原有效免修 w1 满足并说明来源，得到 %+v", st)
	}
	if len(st.PassedEnrollmentIDs) != 1 || st.PassedEnrollmentIDs[0] != "e1" {
		t.Fatalf("原通过修读历史 e1 不应因改名丢失，得到 %v", st.PassedEnrollmentIDs)
	}
	if st.Course == nil || st.Course.Name != "高等数学" || st.Course.Credit != 4 {
		t.Fatalf("核对结果中的课程资料应显示新名称与原学分，得到 %+v", st.Course)
	}
	// 修读与免修记录继续指向原要求，编号不变。
	if e := s.Enrollment("s1", "e1"); e == nil || e.ReqID != "r1" || e.Result != Passed {
		t.Fatalf("已有修读应继续指向原要求 r1 且结果不变，得到 %+v", e)
	}
	if w := s.Waiver("s1", "w1"); w == nil || w.ReqID != "r1" || w.Status != WaiverApproved {
		t.Fatalf("已有免修应继续指向原要求 r1 且仍有效，得到 %+v", w)
	}
}

// TestCourseReregisterReferencedRenameKeepsClosed 停开课程在被引用后
// 以原学分改名：改名成功，但课程仍为停开。
func TestCourseReregisterReferencedRenameKeepsClosed(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "旧名称", 4)
	mustReq(t, s, "s1", "r1", "c1")
	if _, err := s.SetCourseOpen("c1", false); err != nil {
		t.Fatal(err)
	}

	c, a, err := s.AddCourse("c1", "高等数学", 4)
	if err != nil || a != ActionUpdated {
		t.Fatalf("停开课程以原学分改名应成功，得到 action=%v err=%v", a, err)
	}
	if c.Open {
		t.Fatalf("停开课程改名后仍应停开，得到 %+v", c)
	}
	if c.Name != "高等数学" || c.Credit != 4 {
		t.Fatalf("名称应更新、学分不变，得到 %+v", c)
	}
}

// TestCourseReregisterNonPositiveCreditRejected 无论课程是否被引用，
// 零或负数学分都应拒绝；对尚未被引用的已有课程，拒绝时不能顺带改掉
// 名称或开放状态，也不应标记落盘。
func TestCourseReregisterNonPositiveCreditRejected(t *testing.T) {
	for _, bad := range []int{0, -1, -100} {
		t.Run("unreferenced", func(t *testing.T) {
			s := NewStore()
			mustCourse(t, s, "c1", "数学", 4)
			s = reloadFresh(t, s) // 只含课程 c1 的干净记录。
			if _, a, err := s.AddCourse("c1", "高等数学", bad); err == nil || a != "" {
				t.Fatalf("学分 %d 应被拒绝，得到 action=%v err=%v", bad, a, err)
			}
			c := s.Course("c1")
			if c.Name != "数学" || c.Credit != 4 || !c.Open {
				t.Fatalf("非法学分拒绝时不应顺带改掉名称或状态，得到 %+v", c)
			}
			if s.Dirty() {
				t.Fatalf("非法学分拒绝不应标记变更，credit=%d", bad)
			}
		})
		t.Run("referenced", func(t *testing.T) {
			s := NewStore()
			mustStudent(t, s, "s1")
			mustCourse(t, s, "c1", "数学", 4)
			mustReq(t, s, "s1", "r1", "c1")
			if _, _, err := s.AddCourse("c1", "高等数学", bad); err == nil {
				t.Fatalf("已引用课程提交学分 %d 也应被拒绝", bad)
			}
			if c := s.Course("c1"); c.Name != "数学" || c.Credit != 4 {
				t.Fatalf("非法学分拒绝后原资料应保留，得到 %+v", c)
			}
		})
	}
}

// TestCourseReregisterRoundTripPersists 成功的更新与被拒绝后的不变都要
// 经得起保存、重新打开：更新后的名称/学分/状态应恢复，拒绝不影响文件。
func TestCourseReregisterRoundTripPersists(t *testing.T) {
	dir := t.TempDir()

	// 未引用时更新停开课程，保存后恢复：新名称、新学分、仍停开。
	s := NewStore()
	mustCourse(t, s, "c1", "数学", 4)
	if _, err := s.SetCourseOpen("c1", false); err != nil {
		t.Fatal(err)
	}
	if _, a, err := s.AddCourse("c1", "高等数学", 5); err != nil || a != ActionUpdated {
		t.Fatalf("更新失败：action=%v err=%v", a, err)
	}
	path := filepath.Join(dir, "records.json")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("Load: existed=%v err=%v", existed, err)
	}
	c := loaded.Course("c1")
	if c == nil || c.Name != "高等数学" || c.Credit != 5 || c.Open {
		t.Fatalf("重新打开后应恢复新名称、新学分与停开状态，得到 %+v", c)
	}

	// 重新打开后再建立要求并保存，随后再次加载，在干净的内存记录上试图改学分：
	// 仍按已引用拒绝，内存不标记变更、磁盘文件也不变化。
	mustStudent(t, loaded, "s1")
	mustReq(t, loaded, "s1", "r1", "c1")
	if err := loaded.Save(path); err != nil {
		t.Fatalf("第二次 Save: %v", err)
	}
	loaded2, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("第二次 Load: existed=%v err=%v", existed, err)
	}
	if loaded2.Dirty() {
		t.Fatal("刚加载的记录不应处于已变更状态")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := loaded2.AddCourse("c1", "再改个名", 6); err == nil {
		t.Fatal("重新加载后，已引用课程改学分仍应被拒绝")
	}
	if loaded2.Dirty() {
		t.Fatal("拒绝改学分不应产生落盘变更")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("拒绝后记录文件不应变化\nbefore=%s\nafter=%s", before, after)
	}
}
