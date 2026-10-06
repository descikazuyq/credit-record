package credit

import (
	"reflect"
	"testing"
)

// TestZeroValueStoreQueriesEmpty 零值集合在任何登记之前就按空集合响应查询：
// 列表为空、按编号查找得到不存在、核对未登记学生明确表示不存在；这些只读
// 查询都不应触发延迟初始化之外的任何变更。
func TestZeroValueStoreQueriesEmpty(t *testing.T) {
	var s Store

	if courses := s.Courses(); len(courses) != 0 {
		t.Fatalf("零值集合课程列表应为空，得到 %v", courses)
	}
	if students := s.Students(); len(students) != 0 {
		t.Fatalf("零值集合学生列表应为空，得到 %v", students)
	}
	if c := s.Course("c1"); c != nil {
		t.Fatalf("零值集合查找课程应不存在，得到 %v", c)
	}
	if st := s.Student("s1"); st != nil {
		t.Fatalf("零值集合查找学生应不存在，得到 %v", st)
	}
	if r := s.Requirement("s1", "r1"); r != nil {
		t.Fatalf("零值集合查找要求应不存在，得到 %v", r)
	}
	if e := s.Enrollment("s1", "e1"); e != nil {
		t.Fatalf("零值集合查找修读应不存在，得到 %v", e)
	}
	if w := s.Waiver("s1", "w1"); w != nil {
		t.Fatalf("零值集合查找免修应不存在，得到 %v", w)
	}
	rep := s.CheckStudent("s1")
	if rep.Found {
		t.Fatal("零值集合核对未登记学生应明确表示不存在")
	}
	if s.Dirty() {
		t.Fatal("零值集合上的只读查询不应产生变更")
	}
}

// TestZeroValueStoreRejectsBeforeFirstCreate 先查询、先提交非法登记都不影响
// 之后的第一次合法登记；非法登记不留对象、不计变更。
func TestZeroValueStoreRejectsBeforeFirstCreate(t *testing.T) {
	var s Store

	// 先做一组空查询。
	_ = s.Course("c1")
	_ = s.Student("s1")

	// 非法学生登记：空串、纯空白。
	for _, id := range []string{"", " ", "\t", "　"} {
		if st, _, err := s.AddStudent(id); err == nil || st != nil {
			t.Fatalf("AddStudent(%q) 应被拒绝且不返回对象，得到 st=%v err=%v", id, st, err)
		}
	}
	// 非法课程登记：空编号、纯空白编号、空名称、非正整数学分。
	if c, _, err := s.AddCourse("", "数学", 4); err == nil || c != nil {
		t.Fatalf("空课程编号应被拒绝，得到 c=%v err=%v", c, err)
	}
	if c, _, err := s.AddCourse("   ", "数学", 4); err == nil || c != nil {
		t.Fatalf("纯空白课程编号应被拒绝，得到 c=%v err=%v", c, err)
	}
	if c, _, err := s.AddCourse("c1", "   ", 4); err == nil || c != nil {
		t.Fatalf("空课程名称应被拒绝，得到 c=%v err=%v", c, err)
	}
	if c, _, err := s.AddCourse("c1", "数学", 0); err == nil || c != nil {
		t.Fatalf("学分为 0 应被拒绝，得到 c=%v err=%v", c, err)
	}
	if len(s.Students()) != 0 || len(s.Courses()) != 0 {
		t.Fatal("被拒绝的登记不应留下任何对象")
	}
	if s.Dirty() {
		t.Fatal("被拒绝的登记不应被标记成一次成功变更")
	}

	// 第一次合法登记（学生）仍应正常完成。
	st, a, err := s.AddStudent("s1")
	if err != nil || a != ActionCreated || st == nil || st.ID != "s1" {
		t.Fatalf("非法尝试后首次合法登记学生失败：st=%v action=%v err=%v", st, a, err)
	}
	if got := s.Student("s1"); got == nil || got.ID != "s1" {
		t.Fatalf("登记后应能按完整编号查到学生，得到 %v", got)
	}
	if !s.Dirty() {
		t.Fatal("成功登记应记录本次变更")
	}
}

// TestZeroValueStoreFirstCourse 零值集合的第一次登记也可以是课程，且课程
// 带有名称、正整数学分与初始开放状态。
func TestZeroValueStoreFirstCourse(t *testing.T) {
	var s Store

	c, a, err := s.AddCourse("c1", "数学", 4)
	if err != nil || a != ActionCreated || c == nil {
		t.Fatalf("零值集合首次登记课程失败：c=%v action=%v err=%v", c, a, err)
	}
	if c.Name != "数学" || c.Credit != 4 || !c.Open {
		t.Fatalf("新课程应为名称/正整数学分/初始开放，得到 %+v", c)
	}
	if got := s.Course("c1"); got != c {
		t.Fatalf("按完整编号应能查到刚登记的课程，得到 %v", got)
	}
	if courses := s.Courses(); len(courses) != 1 || courses[0] != c {
		t.Fatalf("课程列表应只含新建课程，得到 %v", courses)
	}
}

// TestZeroValueStoreFirstEitherOrder 先登记的对象不能因随后登记另一类对象
// 而丢失；学生先、课程先两种顺序都要成立。
func TestZeroValueStoreFirstEitherOrder(t *testing.T) {
	for _, studentFirst := range []bool{true, false} {
		var s Store
		if studentFirst {
			mustStudent(t, &s, "s1")
			mustCourse(t, &s, "c1", "数学", 4)
		} else {
			mustCourse(t, &s, "c1", "数学", 4)
			mustStudent(t, &s, "s1")
		}
		if got := s.Student("s1"); got == nil || got.ID != "s1" {
			t.Fatalf("studentFirst=%v：学生在另一类对象登记后丢失", studentFirst)
		}
		if got := s.Course("c1"); got == nil || got.Name != "数学" || got.Credit != 4 || !got.Open {
			t.Fatalf("studentFirst=%v：课程在另一类对象登记后丢失或被改动：%v", studentFirst, got)
		}
	}
}

// TestZeroValueStoreFullWorkflow 零值集合上沿用已有对象走完整条业务链：
// 建要求、选课、提交成绩；通过取得课程学分，选课与未通过不取得学分。
func TestZeroValueStoreFullWorkflow(t *testing.T) {
	var s Store
	mustStudent(t, &s, "s1")
	mustCourse(t, &s, "c1", "数学", 4)
	mustCourse(t, &s, "c2", "英语", 2)
	mustReq(t, &s, "s1", "r1", "c1")
	mustReq(t, &s, "s1", "r2", "c2")

	// 对 c2 先选课、未通过；对 c1 通过。
	mustEnroll(t, &s, "s1", "r2", "2024春", "e2")
	if _, changed, err := s.SubmitResult("s1", "e2", Failed); err != nil || !changed {
		t.Fatalf("提交未通过失败：changed=%v err=%v", changed, err)
	}
	mustEnroll(t, &s, "s1", "r1", "2024秋", "e1")
	e, changed, err := s.SubmitResult("s1", "e1", Passed)
	if err != nil || !changed || e.Result != Passed {
		t.Fatalf("提交通过失败：e=%v changed=%v err=%v", e, changed, err)
	}
	// 未通过也占用提交序号：未通过占 1、通过占 2（按提交先后），学分只计
	// 通过的 c1（4 分）。
	if e2 := s.Enrollment("s1", "e2"); e2.ResultSeq != 1 {
		t.Fatalf("未通过也应领取序号，得到 %d", e2.ResultSeq)
	}
	if e.ResultSeq != 2 {
		t.Fatalf("通过记录序号应为 2，得到 %d", e.ResultSeq)
	}

	rep := s.CheckStudent("s1")
	if !rep.Found {
		t.Fatal("学生应当存在")
	}
	if rep.TotalCredits != 4 || rep.Overflow {
		t.Fatalf("只有通过的 c1 计 4 学分，得到 total=%d overflow=%v", rep.TotalCredits, rep.Overflow)
	}
	var unmet []string
	for _, u := range rep.Unmet {
		unmet = append(unmet, u)
	}
	if !reflect.DeepEqual(unmet, []string{"r2"}) {
		t.Fatalf("未满足要求应只剩 r2，得到 %v", unmet)
	}
}

// TestZeroValueStoreWhitespaceAndIdempotency 零值集合上编号匹配与幂等规则
// 与 NewStore 完全一致：“s1”和“ s1 ”是两名独立学生，课程编号同样保留
// 前后空白；同完整编号的重复登记返回原记录、不新增副本、不改变开放状态。
func TestZeroValueStoreWhitespaceAndIdempotency(t *testing.T) {
	var s Store
	mustStudent(t, &s, "s1")
	mustStudent(t, &s, " s1 ")
	if len(s.Students()) != 2 {
		t.Fatalf("“s1”与“ s1 ”应为两名独立学生，列表得到 %v", s.Students())
	}
	if got := s.Student("s1"); got == nil || got.ID != "s1" {
		t.Fatal("应按完整编号命中 s1")
	}
	if got := s.Student(" s1 "); got == nil || got.ID != " s1 " {
		t.Fatal("应按完整编号命中“ s1 ”并保留原文")
	}

	mustCourse(t, &s, "c1", "数学", 4)
	mustCourse(t, &s, " c1 ", " 英语 ", 2)
	if len(s.Courses()) != 2 {
		t.Fatalf("“c1”与“ c1 ”应为两门独立课程，列表得到 %v", s.Courses())
	}
	if got := s.Course(" c1 "); got == nil || got.Name != "英语" || got.Credit != 2 {
		t.Fatalf("带空白课程应保留完整编号并查到自身名称学分，得到 %v", got)
	}

	// 停开 c1 后重复提交同内容：返回原记录、保持停开状态、不计变更。
	closed, err := s.SetCourseOpen("c1", false)
	if err != nil || closed.Open {
		t.Fatalf("停开课程失败：%v err=%v", closed, err)
	}
	before := s.Dirty()
	again, a, err := s.AddCourse("c1", "数学", 4)
	if err != nil || a != ActionExisted || again != closed || again.Open {
		t.Fatalf("重复登记应原样返回且不改变停开状态：%v action=%v err=%v", again, a, err)
	}
	if len(s.Courses()) != 2 {
		t.Fatal("幂等返回不应新增副本")
	}
	if s.Dirty() != before {
		t.Fatal("幂等返回不应改变变更标记")
	}

	// 带空白编号学生的对象不能借用另一名学生的记录：r9 属于“s1”，
	// “ s1 ”名下没有该要求，选课必须按完整编号判定为不存在，而不是借用。
	mustReq(t, &s, "s1", "r9", "c1")
	if _, _, err := s.AddEnrollment(" s1 ", "r9", "2024春", "eX"); err == nil {
		t.Fatal("完整编号不匹配时绝不能借用另一名学生的要求登记修读")
	}
	// 带空白编号学生可分别就两门完整编号不同的课程建立要求：c1 与“ c1 ”
	// 各自独立，去掉空白能碰上另一门课程也不冲突、不借用。
	mustReq(t, &s, " s1 ", "r1", "c1")
	mustReq(t, &s, " s1 ", "r2", " c1 ")
}

// TestZeroValueStoreDirtySemantics 零值集合：登记成功记录变更，查询与被
// 拒绝登记不凭空产生变更；幂等返回同样不算变更。
func TestZeroValueStoreDirtySemantics(t *testing.T) {
	var s Store
	if s.Dirty() {
		t.Fatal("零值集合初始不应有变更")
	}
	_ = s.Student("nobody")
	_ = s.Courses()
	if s.Dirty() {
		t.Fatal("查询不应产生变更")
	}
	if _, _, err := s.AddStudent("  "); err == nil {
		t.Fatal("纯空白编号应被拒绝")
	}
	if s.Dirty() {
		t.Fatal("被拒绝的登记不应产生变更")
	}
	mustStudent(t, &s, "s1")
	if !s.Dirty() {
		t.Fatal("成功登记后应标记变更")
	}
	if _, a, err := s.AddStudent("s1"); err != nil || a != ActionExisted {
		t.Fatalf("重复登记应幂等：action=%v err=%v", a, err)
	}
}

// TestZeroValueStoreParityWithNewStore 零值集合在一轮混合操作后，其全部可
// 观察状态应与同样操作序列下的 NewStore 一致——修复只补齐初始化差异，
// 不改变任何既有登记规则。
func TestZeroValueStoreParityWithNewStore(t *testing.T) {
	ops := func(s *Store) {
		_, _, _ = s.AddStudent("  ")
		_, _, _ = s.AddCourse("c1", "数学", 0)
		_, _, _ = s.AddStudent("s1")
		_, _, _ = s.AddCourse("c1", "数学", 4)
		_, _, _ = s.AddCourse("c1", "数学", 4)
		_, _, _ = s.AddStudent("s2")
		_, _, _ = s.AddRequirement("s1", "r1", "c1")
		_, _, _ = s.AddEnrollment("s1", "r1", "2024秋", "e1")
		_, _, _ = s.SubmitResult("s1", "e1", Passed)
		_, _, _ = s.ApplyWaiver("s2", "rX", "w1", "转学分")
	}

	var zero Store
	made := NewStore()
	ops(&zero)
	ops(made)

	if zero.Dirty() != made.Dirty() {
		t.Fatalf("变更标记不一致：zero=%v made=%v", zero.Dirty(), made.Dirty())
	}
	if len(zero.Students()) != len(made.Students()) ||
		len(zero.Courses()) != len(made.Courses()) ||
		len(zero.AllRequirements()) != len(made.AllRequirements()) {
		t.Fatal("零值集合与 NewStore 的记录数量不一致")
	}
	zRep := zero.CheckStudent("s1")
	mRep := made.CheckStudent("s1")
	if zRep.TotalCredits != mRep.TotalCredits || zRep.Found != mRep.Found {
		t.Fatalf("核对结果不一致：zero=%+v made=%+v", zRep, mRep)
	}
	zRej := zero.CheckStudent("s2")
	if len(zRej.RejectedWaivers) != 1 || zRej.RejectedWaivers[0].Waiver.ID != "w1" {
		t.Fatalf("被拒绝免修历史应保留，得到 %+v", zRej.RejectedWaivers)
	}
}
