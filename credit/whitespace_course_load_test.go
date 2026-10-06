package credit

import (
	"strings"
	"testing"
)

// 本文件为“课程登记按用户给出的完整编号区分课程”提供 store 级回归。
// 课程编号与学生编号、要求编号、修读编号、免修编号采用同一套口径：前后
// 普通空格、制表符、全角空格（U+3000）都是编号内容，绝不修剪。
//   - “c1”与“ c1 ”是两门独立课程，各自保留完整编号、名称与正整数
//     学分，初始开放；
//   - 文件中只有“c1”时，提交“ c1 ”必须新建后者，不能借用前者的名称、
//     学分或停开状态；再次提交某个完整编号只判断对应课程：内容相同返回
//     原记录，内容不同仍按现有规则原地更新，不新增副本、不改动另一门；
//   - 学分锁定跟随实际命中的课程：只要有要求引用了“ c1 ”，它就不能
//     修改学分，即使“c1”未被引用也一样；反过来“c1”被引用不能阻止
//     “ c1 ”更新。整次拒绝时原名称、学分、开放状态全部保留。被引用
//     课程保持原学分时仍允许改名，已有要求、修读与免修继续关联，核对
//     结论不变；重新登记停开课程后仍须停开；
//   - 空字符串编号与全部由空白字符组成的编号仍拒绝，含实际文字的编号
//     原样保存并参与重复判断，拒绝不产生变更。

// courseWhitespaceData 构造同号不同空白的两门课程并存的合法记录：
// "c1"（高等数学 4 学分）与 " c1 "（大学物理 3 学分），均开放。
func courseWhitespaceData() *fileData {
	return &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: " c1 ", Name: "大学物理", Credit: 3, Open: true},
		},
	}
}

// TestAddCourseWhitespaceIDsAreDistinct 首次分别登记 "c1" 与 " c1 " 时
// 保存两门独立课程：完整编号、名称、学分各自保留，初始开放，课程列表中
// 分别可按完整编号取到。
func TestAddCourseWhitespaceIDsAreDistinct(t *testing.T) {
	s := NewStore()

	c1, action, err := s.AddCourse("c1", "高等数学", 4)
	if err != nil || action != ActionCreated || !c1.Open {
		t.Fatalf("登记 c1 应新建且初始开放，action=%v err=%v", action, err)
	}
	// 带前后空白的编号是另一门课程，不能命中已登记的 "c1"。
	c1w, action, err := s.AddCourse(" c1 ", "大学物理", 3)
	if err != nil || action != ActionCreated || !c1w.Open {
		t.Fatalf("登记 \" c1 \" 应作为另一门课程新建，action=%v err=%v", action, err)
	}
	if c1w.ID != " c1 " {
		t.Fatalf("新课程编号应保留完整空白，得到 %q", c1w.ID)
	}

	if c1w == c1 {
		t.Fatal("两门课程不应是同一条记录")
	}
	if got := s.Course("c1"); got != c1 || got.Name != "高等数学" || got.Credit != 4 {
		t.Fatalf("按完整编号 \"c1\" 应取到原课程，得到 %+v", got)
	}
	if got := s.Course(" c1 "); got != c1w || got.Name != "大学物理" || got.Credit != 3 {
		t.Fatalf("按完整编号 \" c1 \" 应取到新课程，得到 %+v", got)
	}
	// 去掉空白或换一种空白写法都取不到另一门。
	if got := s.Course("c1 "); got != nil {
		t.Fatalf("\"c1 \" 是第三门不存在的课程，得到 %+v", got)
	}
	all := s.Courses()
	if len(all) != 2 || all[0].ID != "c1" || all[1].ID != " c1 " {
		t.Fatalf("课程列表应分别显示两门课程且保持登记顺序，得到 %+v", all)
	}
}

// TestAddCourseWhitespaceVariants 普通空格、制表符、全角空格都是编号
// 内容：不同空白写法各自独立；同一种写法重复提交只命中自己。
func TestAddCourseWhitespaceVariants(t *testing.T) {
	s := NewStore()
	for _, id := range []string{"c1", " c1", "c1 ", " c1 ", "\tc1\t", "　c1　"} {
		if _, a, err := s.AddCourse(id, "课程", 1); err != nil || a != ActionCreated {
			t.Fatalf("编号 %q 应作为独立课程新建，action=%v err=%v", id, a, err)
		}
	}
	if got := len(s.Courses()); got != 6 {
		t.Fatalf("六种空白写法应保存六门课程，得到 %d", got)
	}
	// 完全相同的完整编号重复提交：幂等返回原记录，不新增副本。
	s.dirty = false
	before := len(s.Courses())
	c, action, err := s.AddCourse("　c1　", "课程", 1)
	if err != nil || action != ActionExisted || c.ID != "　c1　" {
		t.Fatalf("相同完整编号应幂等返回原记录，action=%v err=%v c=%+v", action, err, c)
	}
	if len(s.Courses()) != before || s.Dirty() {
		t.Fatal("幂等返回不应新增课程或标记变更")
	}
}

// TestAddCourseWhitespaceNoBorrow 文件中只有 "c1" 时提交 " c1 " 应新建
// 后者，不能借用前者的资料；尤其不能继承 "c1" 的停开状态。已有文件中
// 的带空白编号也必须能按原编号维护。
func TestAddCourseWhitespaceNoBorrow(t *testing.T) {
	t.Run("只有c1时提交带空白编号新建", func(t *testing.T) {
		d := &fileData{
			Version: recordVersion,
			Courses: []*Course{
				{ID: "c1", Name: "高等数学", Credit: 4, Open: false},
			},
		}
		s, _ := mustLoadWhitespace(t, d)
		s.dirty = false

		c, action, err := s.AddCourse(" c1 ", "大学物理", 3)
		if err != nil || action != ActionCreated || !c.Open {
			t.Fatalf("\" c1 \" 应新建且按新课程初始开放，action=%v err=%v", action, err)
		}
		if c.ID != " c1 " || c.Name != "大学物理" || c.Credit != 3 {
			t.Fatalf("新课程不应借用 \"c1\" 的资料，得到 %+v", c)
		}
		// 原课程的名称、学分、停开状态保持原样。
		if orig := s.Course("c1"); orig.Name != "高等数学" || orig.Credit != 4 || orig.Open {
			t.Fatalf("原课程 c1 不应被改动，得到 %+v", orig)
		}
		if len(s.Courses()) != 2 {
			t.Fatalf("应保存两门课程，得到 %+v", s.Courses())
		}
	})

	t.Run("已有文件中的带空白编号按原编号维护", func(t *testing.T) {
		s, _ := mustLoadWhitespace(t, courseWhitespaceData())
		s.dirty = false

		// 用 " c1 " 提交相同内容：幂等返回文件里的原记录，不修剪、不合并。
		existing := s.Course(" c1 ")
		got, action, err := s.AddCourse(" c1 ", "大学物理", 3)
		if err != nil || action != ActionExisted || got != existing {
			t.Fatalf("已有 \" c1 \" 相同内容应返回原记录，action=%v err=%v", action, err)
		}
		if s.Dirty() {
			t.Fatal("幂等返回不应标记变更")
		}
		// 用 " c1 " 提交不同名称与学分：在未被引用时原地更新该课程，
		// "c1" 不受影响。
		got, action, err = s.AddCourse(" c1 ", "大学物理（上）", 5)
		if err != nil || action != ActionUpdated {
			t.Fatalf("应原地更新 \" c1 \"，action=%v err=%v", action, err)
		}
		if got.ID != " c1 " || got.Name != "大学物理（上）" || got.Credit != 5 {
			t.Fatalf("更新应保留完整编号并写入新内容，得到 %+v", got)
		}
		if orig := s.Course("c1"); orig.Name != "高等数学" || orig.Credit != 4 {
			t.Fatalf("更新 \" c1 \" 不应改动另一门课程 c1，得到 %+v", orig)
		}
		if len(s.Courses()) != 2 {
			t.Fatal("更新不应新增课程副本")
		}
	})
}

// TestAddCourseWhitespaceIdempotentAndUpdateExact 再次提交某个完整编号时
// 只判断对应课程：名称与学分相同返回原记录；只改名称或名称学分都改时按
// 现有规则更新原课程，不新增副本、不改动另一门课程。
func TestAddCourseWhitespaceIdempotentAndUpdateExact(t *testing.T) {
	s, _ := mustLoadWhitespace(t, courseWhitespaceData())
	s.dirty = false

	// "c1" 同内容幂等；名称用 " c1 " 的名称也不影响判断——编号不同
	// 就是两门课，绝不按名称借用。
	orig := s.Course("c1")
	got, action, err := s.AddCourse("c1", "高等数学", 4)
	if err != nil || action != ActionExisted || got != orig || s.Dirty() {
		t.Fatalf("c1 相同内容应幂等返回原记录，action=%v err=%v dirty=%v", action, err, s.Dirty())
	}

	// 只改 "c1" 的名称、学分不变：只更新 "c1"。
	got, action, err = s.AddCourse("c1", "高等数学（下）", 4)
	if err != nil || action != ActionUpdated || got.Name != "高等数学（下）" || got.Credit != 4 {
		t.Fatalf("c1 只改名应原地更新，action=%v err=%v got=%+v", action, err, got)
	}
	if other := s.Course(" c1 "); other.Name != "大学物理" || other.Credit != 3 {
		t.Fatalf("\" c1 \" 不应被 c1 的更新影响，得到 %+v", other)
	}

	// 文件中只有 "c1" 时，即使提交 " c1 " 的名称与学分恰好与 "c1" 完全
	// 一致，也必须按不同编号新建，不能因为内容一致就返回 "c1"。
	fresh := NewStore()
	if _, a, err := fresh.AddCourse("c1", "高等数学（下）", 4); err != nil || a != ActionCreated {
		t.Fatalf("登记 c1 失败：action=%v err=%v", a, err)
	}
	got, action, err = fresh.AddCourse(" c1 ", "高等数学（下）", 4)
	if err != nil || action != ActionCreated {
		t.Fatalf("编号不同即使名称学分一致也是另一门课，应新建，action=%v err=%v", action, err)
	}
	if got.ID != " c1 " {
		t.Fatalf("新课程应保留完整编号，得到 %q", got.ID)
	}
	if len(fresh.Courses()) != 2 {
		t.Fatalf("应新增第二门课程，得到 %+v", fresh.Courses())
	}
}

// TestAddCourseWhitespaceReferenceLockPerExactID 学分锁定跟随实际命中的
// 课程：引用 " c1 " 只锁定 " c1 "，"c1" 仍可在未被引用时改学分；
// 反过来引用 "c1" 不锁定 " c1 "。被引用课程同时改名改学分整次拒绝，
// 原名称、学分、开放状态全部保留；保持原学分只改名仍允许。
func TestAddCourseWhitespaceReferenceLockPerExactID(t *testing.T) {
	t.Run("引用带空白课程只锁定该完整编号", func(t *testing.T) {
		d := courseWhitespaceData()
		d.Students = []*Student{{ID: "s1"}}
		d.Requirements = []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: " c1 "},
		}
		s, _ := mustLoadWhitespace(t, d)

		// 未被引用的 "c1" 仍可改名改学分。
		if _, a, err := s.AddCourse("c1", "高等数学（下）", 6); err != nil || a != ActionUpdated {
			t.Fatalf("未被引用的 c1 应允许更新，action=%v err=%v", a, err)
		}
		// 被引用的 " c1 " 同时改名改学分：整次拒绝，原资料全保留。
		_, _, err := s.AddCourse(" c1 ", "大学物理（下）", 5)
		if err == nil || !strings.Contains(err.Error(), "已被课程要求引用") ||
			!strings.Contains(err.Error(), "原学分 3") {
			t.Fatalf("被引用的 \" c1 \" 改学分应拒绝并说明原学分，得到 %v", err)
		}
		if got := s.Course(" c1 "); got.Name != "大学物理" || got.Credit != 3 || !got.Open {
			t.Fatalf("整次拒绝后 \" c1 \" 的名称、学分、开放状态应全保留，得到 %+v", got)
		}
		// 拒绝不应影响刚更新成功的 "c1"。
		if got := s.Course("c1"); got.Name != "高等数学（下）" || got.Credit != 6 {
			t.Fatalf("c1 的更新不应受 \" c1 \" 拒绝影响，得到 %+v", got)
		}

		// 保持原学分只改名：允许，已有要求继续关联原课程。
		c, a, err := s.AddCourse(" c1 ", "大学物理（实验班）", 3)
		if err != nil || a != ActionUpdated || c.Name != "大学物理（实验班）" || c.Credit != 3 {
			t.Fatalf("被引用课程保持原学分应允许改名，action=%v err=%v", a, err)
		}
		if r := s.Requirement("s1", "r1"); r.CourseID != " c1 " {
			t.Fatalf("已有要求应继续关联完整编号 \" c1 \"，得到 %+v", r)
		}
		rep := s.CheckStudent("s1")
		if !rep.Found || rep.TotalCredits != 0 || len(rep.Unmet) != 1 {
			t.Fatalf("改名不应改变要求的核对结论（未满足、0 学分），得到 %+v", rep)
		}
	})

	t.Run("引用c1不锁定带空白课程", func(t *testing.T) {
		d := courseWhitespaceData()
		d.Students = []*Student{{ID: "s1"}}
		d.Requirements = []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: "c1"},
		}
		s, _ := mustLoadWhitespace(t, d)

		// "c1" 被引用：改学分拒绝。
		if _, _, err := s.AddCourse("c1", "另一个名称", 9); err == nil {
			t.Fatal("被引用的 c1 改学分应拒绝")
		}
		// " c1 " 没有任何要求引用：改名改学分都允许。
		if _, a, err := s.AddCourse(" c1 ", "大学物理（下）", 7); err != nil || a != ActionUpdated {
			t.Fatalf("未被引用的 \" c1 \" 应允许更新，action=%v err=%v", a, err)
		}
		if got := s.Course("c1"); got.Name != "高等数学" || got.Credit != 4 {
			t.Fatalf("c1 被拒绝后资料应保留，得到 %+v", got)
		}
	})
}

// TestAddCourseWhitespaceClosedCourseStaysClosed 停开的带空白编号课程
// 在未被引用时允许更新，但更新后仍须停开；被引用时拒绝同样保持停开。
func TestAddCourseWhitespaceClosedCourseStaysClosed(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: " c1 ", Name: "大学物理", Credit: 3, Open: false},
		},
		Students:     []*Student{{ID: "s1"}},
		Requirements: nil,
	}
	s, _ := mustLoadWhitespace(t, d)

	c, action, err := s.AddCourse(" c1 ", "大学物理（下）", 6)
	if err != nil || action != ActionUpdated || c.Open {
		t.Fatalf("停开课程更新后应仍停开，action=%v err=%v open=%v", action, err, c.Open)
	}
	if c.Name != "大学物理（下）" || c.Credit != 6 {
		t.Fatalf("名称与学分应已更新，得到 %+v", c)
	}

	// 被引用后改名改学分整次拒绝，停开状态继续保留。
	if _, a, err := s.AddRequirement("s1", "r1", " c1 "); err != nil || a != ActionCreated {
		t.Fatalf("为 \" c1 \" 建立要求失败：action=%v err=%v", a, err)
	}
	if _, _, err := s.AddCourse(" c1 ", "再改名称", 7); err == nil {
		t.Fatal("被引用的停开课程改学分应拒绝")
	}
	if got := s.Course(" c1 "); got.Open || got.Name != "大学物理（下）" || got.Credit != 6 {
		t.Fatalf("拒绝后名称、学分、停开状态应全保留，得到 %+v", got)
	}
	// 停开课程保持原学分只改名：允许，仍停开，要求关联与核对结论不变。
	if c, a, err := s.AddCourse(" c1 ", "大学物理（重修班）", 6); err != nil ||
		a != ActionUpdated || c.Open {
		t.Fatalf("停开课程保持学分只改名应成功且仍停开，action=%v err=%v open=%v",
			a, err, c.Open)
	}
	rep := s.CheckStudent("s1")
	if !rep.Found || rep.TotalCredits != 0 {
		t.Fatalf("改名不应改变核对结论，得到 %+v", rep)
	}
}

// TestAddCourseBlankIDRejected 空字符串编号与全部由空白字符组成的编号
// （普通空格、制表符、全角空格，含混用）一律拒绝，且不产生变更；
// 含实际文字的编号（即使前后带空白）原样保存。
func TestAddCourseBlankIDRejected(t *testing.T) {
	s := NewStore()
	for _, id := range []string{"", " ", "  ", "\t", "　", " \t　"} {
		if _, _, err := s.AddCourse(id, "课程", 4); err == nil {
			t.Fatalf("编号 %q 应被拒绝", id)
		} else if id == "" && !strings.Contains(err.Error(), "不能为空") {
			t.Fatalf("空字符串编号应提示不能为空，得到 %v", err)
		} else if id != "" && !strings.Contains(err.Error(), "空白字符") {
			t.Fatalf("全空白编号 %q 应提示不能只由空白字符组成，得到 %v", id, err)
		}
		if s.Dirty() || len(s.Courses()) != 0 {
			t.Fatalf("拒绝编号 %q 不应产生变更或课程", id)
		}
	}

	// 含实际文字的编号原样保存，并按完整文字参与之后的重复判断。
	c, action, err := s.AddCourse("  c1\t", "课程", 4)
	if err != nil || action != ActionCreated || c.ID != "  c1\t" {
		t.Fatalf("含实际文字的编号应原样保存，action=%v err=%v c=%+v", action, err, c)
	}
	if _, a, err := s.AddCourse("  c1\t", "课程", 4); err != nil || a != ActionExisted {
		t.Fatalf("相同完整编号应幂等，action=%v err=%v", a, err)
	}
	if s.Course("c1") != nil {
		t.Fatal("去空白后的编号不应命中含空白的课程")
	}
}
