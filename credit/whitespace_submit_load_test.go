package credit

import (
	"strings"
	"testing"
)

// 本文件为“提交修读结果时按用户给出的完整编号确定成绩归属”这项行为提供
// 回归保障。记录文件可以合法保存带前后空白的学生编号与修读编号（登记入口
// 同样保留完整编号，既有文件中的编号原文也不得改写，Load 按原样接受），因此
// 全部用例直接构造记录文件，聚焦 pass/fail 的选对象逻辑：
//   - “s1”与“ s1 ”是两名不同学生，同一学生名下“e1”与“ e1 ”是两份
//     不同修读；普通空格、制表符、全角空格（U+3000）都是编号内容；
//   - 给带空白的编号提交结果只命中编号完全一致的那一份，另一名学生或
//     另一份修读的状态、学分与来源保持原样；
//   - 完整编号找不到学生时明确报学生不存在，学生存在但名下没有该完整
//     修读编号时明确报名下不存在该修读；即使去掉空白能碰上另一条记录，
//     也绝不借用那份记录或补建修读；
//   - 含前后空白的编号本身合法，绝不能判为文件损坏；
//   - 准确命中后沿用现有规则：重复提交相同结果幂等返回，改提另一结果拒绝。

// whitespaceBase 构造一份结构完整、引用齐全的合法记录：
// 学生 "s1" 与 " s1 "（前后普通空格）各自的要求 r1 指向 4 学分课程 c1，
// 名下各有一份尚未提交结果的修读 e1。
func whitespaceBase() *fileData {
	return &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
		},
		Students: []*Student{{ID: "s1"}, {ID: " s1 "}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: "c1"},
			{ID: "r1", StudentID: " s1 ", CourseID: "c1"},
		},
		Enrollments: []*Enrollment{
			{ID: "e1", StudentID: "s1", ReqID: "r1", Term: "2024春", Result: Enrolled},
			{ID: "e1", StudentID: " s1 ", ReqID: "r1", Term: "2024春", Result: Enrolled},
		},
	}
}

// mustLoadWhitespace 加载合法记录；含前后空白编号的记录必须被接受，
// 不能判为文件损坏。
func mustLoadWhitespace(t *testing.T, d *fileData) (*Store, string) {
	t.Helper()
	path, _ := writeRecord(t, d)
	s, _, err := Load(path)
	if err != nil {
		t.Fatalf("含前后空白编号的记录是合法记录，不应判为损坏：%v", err)
	}
	return s, path
}

// TestSubmitResultWhitespaceStudentIDsExact 两名学生 "s1" 与 " s1 " 各有
// 一份未提交结果的 e1：给 " s1 " 提交通过只命中本人的修读，核对按本人
// 要求获得学分并以本人的修读说明来源；"s1" 的状态、学分与来源保持原样。
func TestSubmitResultWhitespaceStudentIDsExact(t *testing.T) {
	s, _ := mustLoadWhitespace(t, whitespaceBase())

	e, changed, err := s.SubmitResult(" s1 ", "e1", Passed)
	if err != nil || !changed {
		t.Fatalf("给 \" s1 \" 的 e1 提交通过应成功，changed=%v err=%v", changed, err)
	}
	if e.StudentID != " s1 " || e.ID != "e1" {
		t.Fatalf("命中的应是 \" s1 \" 本人的 e1，得到 %+v", e)
	}

	// " s1 " 本人：要求满足、4 学分、来源为本人修读 e1。
	rep := s.CheckStudent(" s1 ")
	if !rep.Found || rep.TotalCredits != 4 {
		t.Fatalf("\" s1 \" 应凭本人 e1 获得 4 学分，得到 %+v", rep)
	}
	if len(rep.Requirements) != 1 || rep.Requirements[0].Source != "enrollment" ||
		rep.Requirements[0].PassedEnrollmentID != "e1" {
		t.Fatalf("\" s1 \" 的来源应是本人的通过修读 e1，得到 %+v", rep.Requirements)
	}

	// "s1" 保持原样：e1 仍是选课、0 学分、要求未满足。
	if got := s.Enrollment("s1", "e1"); got == nil || got.Result != Enrolled {
		t.Fatalf("\"s1\" 的 e1 不应被改写，得到 %+v", got)
	}
	rep = s.CheckStudent("s1")
	if !rep.Found || rep.TotalCredits != 0 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("\"s1\" 应仍为 0 学分且 r1 未满足，得到 %+v", rep)
	}
}

// TestSubmitResultWhitespaceEnrollmentIDsExact 同一学生名下的 "e1" 与
// " e1 " 是两份独立修读：提交其中一份不改变另一份，既不误报重复提交，
// 也不误报结果冲突。
func TestSubmitResultWhitespaceEnrollmentIDsExact(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
		},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
		Enrollments: []*Enrollment{
			// "e1" 已提交通过，" e1 " 尚未提交结果：提交 " e1 " 时绝不能
			// 因去空白撞上 "e1" 而误报重复提交或结果冲突。
			{ID: "e1", StudentID: "s1", ReqID: "r1", Term: "2024春", Result: Passed, ResultSeq: 1},
			{ID: " e1 ", StudentID: "s1", ReqID: "r1", Term: "2024秋", Result: Enrolled},
		},
		NextResultSeq: 1,
	}
	s, _ := mustLoadWhitespace(t, d)

	e, changed, err := s.SubmitResult("s1", " e1 ", Failed)
	if err != nil || !changed {
		t.Fatalf("给 \" e1 \" 提交未通过应作为独立修读被接受，changed=%v err=%v", changed, err)
	}
	if e.ID != " e1 " || e.Result != Failed || e.Term != "2024秋" {
		t.Fatalf("命中的应是 \" e1 \" 本身且学期不变，得到 %+v", e)
	}
	// 未通过不得学分，但原有通过记录与学分保持原样。
	if got := s.Enrollment("s1", "e1"); got == nil || got.Result != Passed {
		t.Fatalf("原有 \"e1\" 的通过结果不应被改写，得到 %+v", got)
	}
	if rep := s.CheckStudent("s1"); rep.TotalCredits != 4 ||
		rep.Requirements[0].PassedEnrollmentID != "e1" {
		t.Fatalf("学分与来源应保持来自 \"e1\" 的通过，得到 %+v", rep)
	}

	// 两份修读各自独立：对 " e1 " 重复提交未通过幂等返回；改提通过拒绝。
	if _, changed, err := s.SubmitResult("s1", " e1 ", Failed); err != nil || changed {
		t.Fatalf("对 \" e1 \" 重复提交未通过应幂等返回，changed=%v err=%v", changed, err)
	}
	if _, _, err := s.SubmitResult("s1", " e1 ", Passed); err == nil {
		t.Fatal("\" e1 \" 已提交未通过后改提通过应被拒绝")
	}
	if got := s.Enrollment("s1", " e1 "); got.Result != Failed {
		t.Fatalf("拒绝后 \" e1 \" 应保留未通过，得到 %+v", got)
	}
}

// TestSubmitResultWhitespaceNoBorrow 完整编号找不到时必须明确拒绝：
// 即使去掉空白后能碰上另一名学生或另一份修读，也绝不借用那份记录，
// 更不能补建修读。普通空格、制表符、全角空格一样不能被忽略。
func TestSubmitResultWhitespaceNoBorrow(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
		},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
		Enrollments: []*Enrollment{
			{ID: "e1", StudentID: "s1", ReqID: "r1", Term: "2024春", Result: Enrolled},
		},
	}

	// 学生编号带各种前后空白：文件中只有 "s1"，这些都应报学生不存在。
	for _, student := range []string{" s1", "s1 ", " s1 ", "\ts1\t", "　s1　"} {
		s, _ := mustLoadWhitespace(t, d)
		_, _, err := s.SubmitResult(student, "e1", Passed)
		if err == nil || !strings.Contains(err.Error(), "不存在") {
			t.Fatalf("学生 %q 不存在，提交应明确拒绝，得到 %v", student, err)
		}
		if !strings.Contains(err.Error(), "学生") {
			t.Fatalf("学生 %q 找不到时应明确报告学生不存在，得到 %v", student, err)
		}
		// 不能借用 "s1" 的修读，也不能补建记录。
		if got := s.Enrollment("s1", "e1"); got == nil || got.Result != Enrolled {
			t.Fatalf("学生 %q 被拒绝后 \"s1\" 的 e1 不应变化，得到 %+v", student, got)
		}
		if got := s.Enrollment(student, "e1"); got != nil {
			t.Fatalf("不应为学生 %q 补建修读，得到 %+v", student, got)
		}
		if s.Dirty() {
			t.Fatalf("被拒绝的提交不应产生变更（学生 %q）", student)
		}
	}

	// 学生存在但修读编号带各种前后空白：应报该学生名下不存在该修读。
	for _, enrID := range []string{" e1", "e1 ", " e1 ", "\te1\t", "　e1　"} {
		s, _ := mustLoadWhitespace(t, d)
		_, _, err := s.SubmitResult("s1", enrID, Passed)
		if err == nil || !strings.Contains(err.Error(), "不存在") {
			t.Fatalf("修读 %q 不存在于 s1 名下，提交应明确拒绝，得到 %v", enrID, err)
		}
		if !strings.Contains(err.Error(), "s1") {
			t.Fatalf("错误应点名学生 s1 名下不存在该修读，得到 %v", err)
		}
		if got := s.Enrollment("s1", "e1"); got == nil || got.Result != Enrolled {
			t.Fatalf("修读 %q 被拒绝后 \"e1\" 不应变化，得到 %+v", enrID, got)
		}
		if got := s.Enrollment("s1", enrID); got != nil {
			t.Fatalf("不应补建修读 %q，得到 %+v", enrID, got)
		}
		if s.Dirty() {
			t.Fatalf("被拒绝的提交不应产生变更（修读 %q）", enrID)
		}
	}
}

// TestSubmitResultWhitespaceIdempotentAndConflictRules 准确命中带空白的
// 修读后沿用现有规则：重复提交相同结果返回原记录且不产生变更；已提交
// 结果改提另一结果拒绝并保留原结果。
func TestSubmitResultWhitespaceIdempotentAndConflictRules(t *testing.T) {
	s, _ := mustLoadWhitespace(t, whitespaceBase())

	if _, changed, err := s.SubmitResult(" s1 ", "e1", Passed); err != nil || !changed {
		t.Fatalf("首次提交通过应成功，changed=%v err=%v", changed, err)
	}
	s.dirty = false // 只看后续提交是否产生新变更

	// 重复提交相同结果：幂等返回原记录，不再产生变更。
	e, changed, err := s.SubmitResult(" s1 ", "e1", Passed)
	if err != nil || changed {
		t.Fatalf("重复提交相同结果应幂等返回，changed=%v err=%v", changed, err)
	}
	if e.StudentID != " s1 " || e.Result != Passed {
		t.Fatalf("幂等返回的应是原记录，得到 %+v", e)
	}
	if s.Dirty() {
		t.Fatal("幂等重复提交不应产生变更")
	}

	// 改提另一结果：拒绝并保留通过。
	if _, _, err := s.SubmitResult(" s1 ", "e1", Failed); err == nil {
		t.Fatal("已提交通过后改提未通过应被拒绝")
	}
	if got := s.Enrollment(" s1 ", "e1"); got.Result != Passed {
		t.Fatalf("拒绝后应保留通过结果，得到 %+v", got)
	}
	if s.Dirty() {
		t.Fatal("被拒绝的改提不应产生变更")
	}
}
