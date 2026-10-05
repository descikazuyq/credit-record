package credit

import (
	"strings"
	"testing"
)

// 本文件回归“提交修读结果时按完整编号识别归属”：学生编号与修读编号前后的
// 空白（普通空格、制表符、全角空格 U+3000 等）本身就是内容，pass/fail 不
// 得修剪。登记入口仍会修剪编号，所以含前后空白的编号只能经合法记录文件
// 产生——这类记录合法，不能判损坏，提交结果时必须与查询使用同一套完整键。

// whitespaceIDRecord 构造三名相关归属共存的合法记录：
//   - 学生 "s1"：要求 r1 指向 4 学分课程 c1、要求 r2 指向 3 学分课程 c2，
//     名下有修读 "e1"（2024春，选课）与 " e1 "（2024秋，选课）；
//   - 学生 " s1 "：要求 r1 指向 c1，名下有修读 "e1"（2024春，选课）。
//
// 去掉编号前后空白后，" s1 " 会与 "s1" 混淆，" e1 " 会与 "e1" 混淆；
// 三者初始都没有提交结果。
func whitespaceIDRecord() *fileData {
	return &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s1"}, {ID: " s1 "}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: "c1"},
			{ID: "r2", StudentID: "s1", CourseID: "c2"},
			{ID: "r1", StudentID: " s1 ", CourseID: "c1"},
		},
		Enrollments: []*Enrollment{
			{ID: "e1", StudentID: "s1", ReqID: "r1", Term: "2024春", Result: Enrolled},
			{ID: " e1 ", StudentID: "s1", ReqID: "r2", Term: "2024秋", Result: Enrolled},
			{ID: "e1", StudentID: " s1 ", ReqID: "r1", Term: "2024春", Result: Enrolled},
		},
	}
}

func loadWhitespaceRecord(t *testing.T) *Store {
	t.Helper()
	path, _ := writeRecord(t, whitespaceIDRecord())
	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("含前后空白编号的合法记录必须正常读取，existed=%v err=%v", existed, err)
	}
	return s
}

// TestSubmitResultPaddedStudentTargetsOnlyThatStudent 题目主场景：给
// " s1 " 提交通过，只有这名学生的 e1 变为通过；核对按其本人要求获得学分、
// 以本人的修读说明来源。"s1" 的修读状态、学分与来源保持原样。
func TestSubmitResultPaddedStudentTargetsOnlyThatStudent(t *testing.T) {
	s := loadWhitespaceRecord(t)

	e, changed, err := s.SubmitResult(" s1 ", "e1", Passed)
	if err != nil || !changed {
		t.Fatalf("给 \" s1 \" 提交通过应成功，changed=%v err=%v", changed, err)
	}
	if e.StudentID != " s1 " || e.ID != "e1" || e.Result != Passed || e.ResultSeq <= 0 {
		t.Fatalf("应只更新 \" s1 \" 本人的 e1，得到 %+v", e)
	}

	// 本人核对：4 学分、要求满足、来源是本人 e1。
	rep := s.CheckStudent(" s1 ")
	if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
		t.Fatalf("\" s1 \" 应凭本人 e1 获得 4 学分，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	st := rep.Requirements[0]
	if st.Req.ID != "r1" || st.Source != "enrollment" || st.PassedEnrollmentID != "e1" ||
		len(st.PassedEnrollmentIDs) != 1 || st.PassedEnrollmentIDs[0] != "e1" {
		t.Fatalf("\" s1 \" 的学分来源应是本人 e1，得到 %+v", st)
	}

	// "s1" 的两份修读都仍是选课，r1/r2 都未满足、0 学分，来源字段为空。
	if got := s.Enrollment("s1", "e1"); got == nil || got.Result != Enrolled {
		t.Fatalf("\"s1\" 的 e1 必须保持选课，得到 %+v", got)
	}
	if got := s.Enrollment("s1", " e1 "); got == nil || got.Result != Enrolled {
		t.Fatalf("\"s1\" 的 \" e1 \" 必须保持选课，得到 %+v", got)
	}
	repS1 := s.CheckStudent("s1")
	if repS1.TotalCredits != 0 || len(repS1.Unmet) != 2 {
		t.Fatalf("\"s1\" 应保持 0 学分、两项要求未满足，得到 %+v", repS1)
	}
	for _, st := range repS1.Requirements {
		if st.Satisfied || st.Source != "" || st.PassedEnrollmentID != "" ||
			len(st.PassedEnrollmentIDs) != 0 {
			t.Fatalf("\"s1\" 不应出现任何满足来源，却得到 %+v", st)
		}
	}

	// 重复提交相同结果：按本人 e1 幂等命中，不与 "s1" 的 e1 混为一次。
	if e2, changed, err := s.SubmitResult(" s1 ", "e1", Passed); err != nil || changed || e2 != e {
		t.Fatalf("\" s1 \" 重复通过应返回原记录且不再变更，得到 %+v changed=%v err=%v",
			e2, changed, err)
	}
	if s.Enrollment("s1", "e1").Result != Enrolled {
		t.Fatal("幂等命中不得改动 \"s1\" 的 e1")
	}
}

// TestSubmitResultPaddedEnrollmentIDSplit 同一学生名下 "e1" 与 " e1 " 是
// 两次独立修读：分别提交、各自记结果与序号，不能误判为重复提交或结果冲突。
func TestSubmitResultPaddedEnrollmentIDSplit(t *testing.T) {
	s := loadWhitespaceRecord(t)

	if _, changed, err := s.SubmitResult("s1", "e1", Passed); err != nil || !changed {
		t.Fatalf("s1 提交 e1 通过应成功，changed=%v err=%v", changed, err)
	}
	// 提交 " e1 " 必须是独立的新结果，而不是 e1 的重复提交或冲突改结果。
	e2, changed, err := s.SubmitResult("s1", " e1 ", Passed)
	if err != nil || !changed {
		t.Fatalf("s1 提交 \" e1 \" 通过应作为独立修读成功，changed=%v err=%v",
			changed, err)
	}
	if e2.ID != " e1 " || e2.ReqID != "r2" || e2.Term != "2024秋" || e2.Result != Passed {
		t.Fatalf("\" e1 \" 的编号、要求与学期必须保持原文，得到 %+v", e2)
	}
	if e1 := s.Enrollment("s1", "e1"); e1.ResultSeq == e2.ResultSeq || e1.Result != Passed {
		t.Fatalf("两份修读应各自取得独立结果与序号，得到 %+v 与 %+v", e1, e2)
	}

	// 两份各计其指向课程的学分一次：r1 经 e1 得 4，r2 经 " e1 " 得 3。
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 7 || len(rep.Unmet) != 0 {
		t.Fatalf("s1 应凭两次独立通过获得 4+3=7 学分，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	sources := map[string]string{}
	passed := map[string][]string{}
	for _, st := range rep.Requirements {
		sources[st.Req.ID] = st.PassedEnrollmentID
		passed[st.Req.ID] = st.PassedEnrollmentIDs
	}
	if sources["r1"] != "e1" || len(passed["r1"]) != 1 || passed["r1"][0] != "e1" {
		t.Fatalf("r1 的来源应是 e1，得到 %v / %v", sources["r1"], passed["r1"])
	}
	if sources["r2"] != " e1 " || len(passed["r2"]) != 1 || passed["r2"][0] != " e1 " {
		t.Fatalf("r2 的来源应是 \" e1 \"，得到 %v / %v", sources["r2"], passed["r2"])
	}

	// 各自改提另一结果仍按本人记录拒绝：不能借另一份修读“通过”掩盖冲突。
	if _, _, err := s.SubmitResult("s1", "e1", Failed); err == nil ||
		!strings.Contains(err.Error(), "e1") {
		t.Fatalf("e1 已通过后改未通过应拒绝并点名 e1，得到 %v", err)
	}
	if _, _, err := s.SubmitResult("s1", " e1 ", Failed); err == nil ||
		!strings.Contains(err.Error(), " e1 ") {
		t.Fatalf("\" e1 \" 已通过后改未通过应拒绝并点名 \" e1 \"，得到 %v", err)
	}
	if s.Enrollment("s1", "e1").Result != Passed ||
		s.Enrollment("s1", " e1 ").Result != Passed {
		t.Fatal("冲突拒绝后原有通过结果必须保留")
	}
	// 各自重复提交相同结果仍幂等。
	if _, changed, err := s.SubmitResult("s1", "e1", Passed); err != nil || changed {
		t.Fatalf("e1 重复通过应幂等，changed=%v err=%v", changed, err)
	}
	if _, changed, err := s.SubmitResult("s1", " e1 ", Passed); err != nil || changed {
		t.Fatalf("\" e1 \" 重复通过应幂等，changed=%v err=%v", changed, err)
	}
}

// TestSubmitResultPaddedEnrollmentFailKeepsRecord 对带空白编号的修读提交
// 未通过：只记录该份修读未通过，不得学分，原要求与学期不变。
func TestSubmitResultPaddedEnrollmentFailKeepsRecord(t *testing.T) {
	s := loadWhitespaceRecord(t)

	e, changed, err := s.SubmitResult("s1", " e1 ", Failed)
	if err != nil || !changed {
		t.Fatalf("对 \" e1 \" 提交未通过应成功，changed=%v err=%v", changed, err)
	}
	if e.ID != " e1 " || e.Result != Failed || e.ReqID != "r2" || e.Term != "2024秋" {
		t.Fatalf("未通过只应记录结果，编号原文、要求与学期保持不变，得到 %+v", e)
	}
	// 未通过不得学分；r2 未满足，r1 也仍是选课，另一份 e1 不受影响。
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 0 {
		t.Fatalf("未通过不计学分，得到 %d", rep.TotalCredits)
	}
	if len(rep.Unmet) != 2 {
		t.Fatalf("r1、r2 都应未满足，得到 %v", rep.Unmet)
	}
	if got := s.Enrollment("s1", "e1"); got.Result != Enrolled {
		t.Fatalf("另一份 e1 必须保持选课，得到 %s", got.Result)
	}
	// 该份重复提交未通过幂等；改提通过仍拒绝；独立的 e1 仍可正常通过。
	if _, changed, err := s.SubmitResult("s1", " e1 ", Failed); err != nil || changed {
		t.Fatalf("\" e1 \" 重复未通过应幂等，changed=%v err=%v", changed, err)
	}
	if _, _, err := s.SubmitResult("s1", " e1 ", Passed); err == nil {
		t.Fatal("\" e1 \" 未通过后改通过应拒绝")
	}
	if _, changed, err := s.SubmitResult("s1", "e1", Passed); err != nil || !changed {
		t.Fatalf("独立的 e1 不应受另一份修读的未通过影响，changed=%v err=%v",
			changed, err)
	}
}

// TestSubmitResultWhitespaceNotFoundRejects 即使去掉空白后能对上另一名学生
// 或另一份修读，也必须按完整编号判定不存在而拒绝：不借用、不补建、不写盘。
func TestSubmitResultWhitespaceNotFoundRejects(t *testing.T) {
	// 完整学生编号不存在：文件里只有 "s1" 与 " s1 "，两侧空白数量/种类不同
	// 的编号都是另一个人。普通空格、制表符、全角空格都不能被忽略。
	for _, student := range []string{"  s1  ", "\ts1", "s1\t", "　s1　", " \ts1 \t"} {
		s := loadWhitespaceRecord(t)
		_, _, err := s.SubmitResult(student, "e1", Passed)
		if err == nil || !strings.Contains(err.Error(), "不存在") ||
			!strings.Contains(err.Error(), student) {
			t.Fatalf("完整学生编号 %q 不存在必须明确拒绝（不得借用去空白后的 s1），得到 %v",
				student, err)
		}
		if s.Dirty() {
			t.Fatalf("拒绝学生 %q 不得标记变更或写文件", student)
		}
		if s.Enrollment("s1", "e1").Result != Enrolled ||
			s.Enrollment(" s1 ", "e1").Result != Enrolled {
			t.Fatalf("拒绝学生 %q 后任何人的修读都不得改变", student)
		}
	}

	// 学生存在但其名下没有完整修读编号：" s1 " 名下只有 e1，没有 " e1 "
	// （" e1 " 属于 s1）；"\te1\t" 去掉制表符像 e1 但并不存在。
	s := loadWhitespaceRecord(t)
	for _, enr := range []string{" e1 ", "\te1", "e1\t", "　e1　"} {
		_, _, err := s.SubmitResult(" s1 ", enr, Passed)
		if err == nil || !strings.Contains(err.Error(), "不存在") ||
			!strings.Contains(err.Error(), " s1 ") || !strings.Contains(err.Error(), enr) {
			t.Fatalf("\" s1 \" 名下完整修读编号 %q 不存在必须明确拒绝，得到 %v",
				enr, err)
		}
		if e := s.Enrollment(" s1 ", enr); e != nil {
			t.Fatalf("不得在 \" s1 \" 名下补建修读 %q，得到 %+v", enr, e)
		}
	}
	if s.Dirty() {
		t.Fatal("名下无此修读的拒绝不得标记变更或写文件")
	}
	// s1 的 " e1 " 与两人各自的 e1 都必须保持选课。
	if s.Enrollment("s1", " e1 ").Result != Enrolled ||
		s.Enrollment("s1", "e1").Result != Enrolled ||
		s.Enrollment(" s1 ", "e1").Result != Enrolled {
		t.Fatal("拒绝借用后所有既有修读必须保持选课")
	}
}

// TestSubmitResultWhitespaceIDsPersistVerbatim 提交结果后保存重开，编号原文
// （含前后空白）与学生归属逐字保留，不带空白的编号不被改写。
func TestSubmitResultWhitespaceIDsPersistVerbatim(t *testing.T) {
	path, _ := writeRecord(t, whitespaceIDRecord())
	s, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SubmitResult(" s1 ", "e1", Passed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SubmitResult("s1", " e1 ", Failed); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("重存后应仍可正常加载：%v", err)
	}
	if e := reloaded.Enrollment(" s1 ", "e1"); e == nil || e.Result != Passed {
		t.Fatalf("\" s1 \" 的通过结果应原样恢复，得到 %+v", e)
	}
	if e := reloaded.Enrollment("s1", " e1 "); e == nil || e.Result != Failed ||
		e.ReqID != "r2" || e.Term != "2024秋" {
		t.Fatalf("s1 的 \" e1 \" 未通过及原要求/学期应原样恢复，得到 %+v", e)
	}
	if e := reloaded.Enrollment("s1", "e1"); e == nil || e.Result != Enrolled {
		t.Fatalf("s1 的 e1 应保持选课，得到 %+v", e)
	}
	students := map[string]bool{}
	for _, st := range reloaded.Students() {
		students[st.ID] = true
	}
	if !students["s1"] || !students[" s1 "] {
		t.Fatalf("两名学生的编号原文都必须保留，得到 %v", students)
	}
}

// TestSubmitResultPlainIDsUnchanged 不含前后空白的普通编号行为不变：
// 不存在的学生/修读仍按原有方式拒绝。
func TestSubmitResultPlainIDsUnchanged(t *testing.T) {
	s := loadWhitespaceRecord(t)
	if _, _, err := s.SubmitResult("nobody", "e1", Passed); err == nil ||
		!strings.Contains(err.Error(), "nobody") || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("不存在的普通学生编号应照常拒绝，得到 %v", err)
	}
	if _, _, err := s.SubmitResult("s1", "nope", Passed); err == nil ||
		!strings.Contains(err.Error(), "nope") {
		t.Fatalf("学生名下不存在的普通修读编号应照常拒绝，得到 %v", err)
	}
	if s.Dirty() {
		t.Fatal("只读式拒绝不应标记变更")
	}
	if _, changed, err := s.SubmitResult("s1", "e1", Passed); err != nil || !changed {
		t.Fatalf("普通编号提交通过应照常成功，changed=%v err=%v", changed, err)
	}
}
