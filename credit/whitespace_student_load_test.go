package credit

import (
	"strings"
	"testing"
)

// 本文件为“学生登记按用户给出的完整编号确定学生身份”提供 store 级回归。
// 学生编号与课程编号、要求编号、修读编号、免修编号采用同一套口径：前后
// 普通空格、制表符、全角空格（U+3000）、不换行空格（U+00A0）都是编号
// 内容，绝不修剪。
//   - “s1”与“ s1 ”是两名独立学生，各自保留完整编号：文件中只有“s1”
//     时登记“ s1 ”新建后者，只有“ s1 ”时登记“s1”也新建独立记录，
//     谁都不继承另一名学生名下的课程要求、修读、免修或学分；
//   - 两名学生都已存在时，再次登记任意一个完整编号都幂等返回本人，
//     不新增副本、不标记变更，即使两人名下有相同编号的要求、修读、
//     免修，这些历史也不移动、不合并；
//   - 已有合法文件中的带空白学生编号按原文使用，登记不能把它们改名；
//   - 空字符串与全部由空白字符（普通空格、制表符、全角空格、不换行
//     空格混用）组成的编号一律拒绝，不创建学生、不标记变更；
//   - 新登记的带空白编号立即与 CheckStudent/Student 等查询口径一致：
//     尚无要求时核对得到 0 学分，编号相近的另一名学生仍显示自己的记录。

// studentWhitespaceData 构造两名学生并存的合法记录。"s1" 名下有要求
// r1（4 学分课程 c1）、已通过修读 e1 与有效免修 w1（指向另一要求
// r2）；" s1 " 是没有任何记录的独立学生。
func studentWhitespaceData() *fileData {
	return &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "大学物理", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s1"}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: "c1"},
			{ID: "r2", StudentID: "s1", CourseID: "c2"},
		},
		Enrollments: []*Enrollment{
			{ID: "e1", StudentID: "s1", ReqID: "r1", Term: "2024春",
				Result: Passed, ResultSeq: 1},
		},
		Waivers: []*Waiver{
			{ID: "w1", StudentID: "s1", ReqID: "r2", Basis: "竞赛获奖",
				Status: WaiverApproved},
		},
		NextResultSeq: 1,
	}
}

// TestAddStudentWhitespaceIDIsDistinct 文件中只有 "s1" 时登记 " s1 "
// 必须新建另一名学生并保留完整编号；新生不继承 "s1" 的任何要求、修读、
// 免修或学分，"s1" 的记录保持原样。
func TestAddStudentWhitespaceIDIsDistinct(t *testing.T) {
	s, _ := mustLoadWhitespace(t, studentWhitespaceData())
	s.dirty = false

	st, action, err := s.AddStudent(" s1 ")
	if err != nil || action != ActionCreated {
		t.Fatalf("只有 \"s1\" 时登记 \" s1 \" 应新建，action=%v err=%v", action, err)
	}
	if st.ID != " s1 " {
		t.Fatalf("新学生应保留完整编号，得到 %q", st.ID)
	}
	if !s.Dirty() {
		t.Fatal("新建学生应标记变更")
	}
	if got := s.Student(" s1 "); got != st {
		t.Fatalf("按完整编号 \" s1 \" 应取到新学生，得到 %+v", got)
	}
	if got := s.Student("s1"); got == nil || got.ID != "s1" || got == st {
		t.Fatalf("\"s1\" 应仍是原来的另一名学生，得到 %+v", got)
	}
	if len(s.Students()) != 2 {
		t.Fatalf("应存在两名学生，得到 %+v", s.Students())
	}

	// 新生名下没有任何要求、修读、免修，核对为 0 学分。
	if rs := s.Requirements(" s1 "); len(rs) != 0 {
		t.Fatalf("新生不应继承要求，得到 %+v", rs)
	}
	if es := s.Enrollments(" s1 "); len(es) != 0 {
		t.Fatalf("新生不应继承修读，得到 %+v", es)
	}
	if ws := s.Waivers(" s1 "); len(ws) != 0 {
		t.Fatalf("新生不应继承免修，得到 %+v", ws)
	}
	rep := s.CheckStudent(" s1 ")
	if !rep.Found || rep.TotalCredits != 0 || len(rep.Requirements) != 0 || len(rep.Unmet) != 0 {
		t.Fatalf("新生核对应为 0 学分且无要求，得到 %+v", rep)
	}

	// "s1" 的要求、通过修读、有效免修与 7 学分（4+3）保持原样。
	if len(s.Requirements("s1")) != 2 || len(s.Enrollments("s1")) != 1 || len(s.Waivers("s1")) != 1 {
		t.Fatalf("\"s1\" 的原有记录不应被移动，req=%v enr=%v waiver=%v",
			s.Requirements("s1"), s.Enrollments("s1"), s.Waivers("s1"))
	}
	rep = s.CheckStudent("s1")
	if !rep.Found || rep.TotalCredits != 7 {
		t.Fatalf("\"s1\" 应仍为 7 学分，得到 %+v", rep)
	}
}

// TestAddStudentWhitespaceIDReverseOnlyPaddedExists 文件中只有 " s1 "
// （且名下有记录）时登记 "s1" 同样新建独立记录，不借用前者任何资料。
func TestAddStudentWhitespaceIDReverseOnlyPaddedExists(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
		},
		Students: []*Student{{ID: " s1 "}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: " s1 ", CourseID: "c1"},
		},
		Enrollments: []*Enrollment{
			{ID: "e1", StudentID: " s1 ", ReqID: "r1", Term: "2024春",
				Result: Passed, ResultSeq: 1},
		},
		NextResultSeq: 1,
	}
	s, _ := mustLoadWhitespace(t, d)

	st, action, err := s.AddStudent("s1")
	if err != nil || action != ActionCreated || st.ID != "s1" {
		t.Fatalf("只有 \" s1 \" 时登记 \"s1\" 应新建独立记录，%+v action=%v err=%v",
			st, action, err)
	}
	if len(s.Requirements("s1")) != 0 || s.CheckStudent("s1").TotalCredits != 0 {
		t.Fatal("新建的 \"s1\" 不应继承 \" s1 \" 的要求或学分")
	}
	rep := s.CheckStudent(" s1 ")
	if rep.TotalCredits != 4 || len(rep.Requirements) != 1 {
		t.Fatalf("\" s1 \" 的 4 学分与要求应保持原样，得到 %+v", rep)
	}
}

// TestAddStudentWhitespaceVariants 普通空格、制表符、全角空格、不换行
// 空格都是编号内容：不同写法各自独立；同一种写法重复提交只命中自己。
func TestAddStudentWhitespaceVariants(t *testing.T) {
	s := NewStore()
	ids := []string{"s1", " s1", "s1 ", " s1 ", "\ts1\t", "　s1　", " s1 "}
	for _, id := range ids {
		if _, a, err := s.AddStudent(id); err != nil || a != ActionCreated {
			t.Fatalf("编号 %q 应作为独立学生新建，action=%v err=%v", id, a, err)
		}
	}
	if got := len(s.Students()); got != len(ids) {
		t.Fatalf("%d 种写法应保存 %d 名学生，得到 %d", len(ids), len(ids), got)
	}

	s.dirty = false
	st, action, err := s.AddStudent("　s1　")
	if err != nil || action != ActionExisted || st.ID != "　s1　" {
		t.Fatalf("相同完整编号应幂等返回原学生，action=%v err=%v st=%+v", action, err, st)
	}
	if s.Dirty() || len(s.Students()) != len(ids) {
		t.Fatal("幂等返回不应新增学生或标记变更")
	}
	// 去掉或改换空白的写法取不到别人。
	if got := s.Student("s1"); got == nil || got.ID != "s1" {
		t.Fatalf("\"s1\" 应只命中自己，得到 %+v", got)
	}
	if got := s.Student("\ts1"); got != nil {
		t.Fatalf("\"\ts1\" 不应命中任何学生，得到 %+v", got)
	}
}

// TestAddStudentWhitespaceIdempotentKeepsHistoriesApart 两名学生都存在
// 且名下有相同编号的要求、修读、免修时，重复登记任一完整编号只确认对应
// 学生存在：返回本人、不标记变更，历史不移动、不合并。
func TestAddStudentWhitespaceIdempotentKeepsHistoriesApart(t *testing.T) {
	d := &fileData{
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
			{ID: "e1", StudentID: "s1", ReqID: "r1", Term: "2024春",
				Result: Passed, ResultSeq: 1},
			{ID: "e1", StudentID: " s1 ", ReqID: "r1", Term: "2024春",
				Result: Enrolled},
		},
		Waivers: []*Waiver{
			{ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "获奖",
				Status: WaiverApproved},
			{ID: "w1", StudentID: " s1 ", ReqID: "r1", Basis: "学科竞赛",
				Status: WaiverRejected, Reason: "该要求已有有效免修 w1"},
		},
		NextResultSeq: 1,
	}
	s, path := mustLoadWhitespace(t, d)
	padded := s.Student(" s1 ")
	plain := s.Student("s1")

	for _, id := range []string{" s1 ", "s1", " s1 "} {
		s.dirty = false
		got, action, err := s.AddStudent(id)
		if err != nil || action != ActionExisted {
			t.Fatalf("重复登记 %q 应幂等返回，action=%v err=%v", id, action, err)
		}
		want := plain
		if id == " s1 " {
			want = padded
		}
		if got != want {
			t.Fatalf("登记 %q 应命中编号完全一致的原学生，得到 %+v", id, got)
		}
		if s.Dirty() {
			t.Fatalf("重复登记 %q 不应标记变更", id)
		}
	}

	// 两名学生各自的同号历史仍归属本人，未被移动或合并。
	if rs := s.Requirements(" s1 "); len(rs) != 1 || rs[0].StudentID != " s1 " {
		t.Fatalf("\" s1 \" 的要求应仍在本人名下，得到 %+v", rs)
	}
	if es := s.Enrollments(" s1 "); len(es) != 1 || es[0].StudentID != " s1 " ||
		es[0].Result != Enrolled {
		t.Fatalf("\" s1 \" 的修读应保持选课且仍在本人名下，得到 %+v", es)
	}
	if ws := s.Waivers(" s1 "); len(ws) != 1 || ws[0].StudentID != " s1 " ||
		ws[0].Status != WaiverRejected {
		t.Fatalf("\" s1 \" 的被拒绝免修应保持原样，得到 %+v", ws)
	}
	if ws := s.Waivers("s1"); len(ws) != 1 || ws[0].Status != WaiverApproved {
		t.Fatalf("\"s1\" 的有效免修应保持原样，得到 %+v", ws)
	}
	if rep := s.CheckStudent(" s1 "); rep.TotalCredits != 0 || len(rep.Unmet) != 1 {
		t.Fatalf("\" s1 \" 应仍为 0 学分且 r1 未满足，得到 %+v", rep)
	}
	if rep := s.CheckStudent("s1"); rep.TotalCredits != 4 {
		t.Fatalf("\"s1\" 应仍为 4 学分，得到 %+v", rep)
	}

	// 幂等登记不改名、不改写归属：重新保存加载后两名学生及各自历史不变。
	if err := s.Save(path); err != nil {
		t.Fatalf("保存失败：%v", err)
	}
	reloaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("重新加载失败：%v", err)
	}
	if reloaded.Student(" s1 ") == nil || reloaded.Student("s1") == nil {
		t.Fatal("重新加载后两名学生都应按原编号存在")
	}
	if es := reloaded.Enrollments(" s1 "); len(es) != 1 || es[0].StudentID != " s1 " {
		t.Fatalf("重新加载后 \" s1 \" 的历史归属不得改变，得到 %+v", es)
	}
}

// TestAddStudentWhitespaceLoadedRecordNotRenamed 已有合法文件中的带空白
// 学生编号继续按原文使用：重复登记命中本人而不是新建，编号不被改名。
func TestAddStudentWhitespaceLoadedRecordNotRenamed(t *testing.T) {
	d := &fileData{
		Version:  recordVersion,
		Students: []*Student{{ID: "　s1　"}},
	}
	s, _ := mustLoadWhitespace(t, d)
	s.dirty = false

	got, action, err := s.AddStudent("　s1　")
	if err != nil || action != ActionExisted || got.ID != "　s1　" {
		t.Fatalf("文件中的带空白编号应按原文幂等命中，%+v action=%v err=%v",
			got, action, err)
	}
	if s.Dirty() || len(s.Students()) != 1 {
		t.Fatal("幂等命中不应改名、新增或标记变更")
	}
}

// TestAddStudentBlankRejected 空字符串与全部由空白字符组成的编号（普通
// 空格、制表符、全角空格、不换行空格，允许混用、允许夹带换行）一律拒绝，
// 错误说明学生编号为空，不创建学生、不标记变更。
func TestAddStudentBlankRejected(t *testing.T) {
	cases := []string{
		"",
		" ",
		"\t",
		"　",
		" ",
		"\n\r",
		" \t　 \n\r",
	}
	for _, id := range cases {
		s := NewStore()
		st, _, err := s.AddStudent(id)
		if err == nil || !strings.Contains(err.Error(), "学生编号不能为空") {
			t.Fatalf("编号 %q 应报学生编号不能为空，得到 st=%+v err=%v", id, st, err)
		}
		if st != nil {
			t.Fatalf("编号 %q 被拒后不应返回学生，得到 %+v", id, st)
		}
		if len(s.Students()) != 0 || s.Dirty() {
			t.Fatalf("编号 %q 被拒后不应创建学生或标记变更", id)
		}
	}

	// 拒绝纯空白编号不能误伤已存在的学生，也不能挡住后续正常登记。
	s, _ := mustLoadWhitespace(t, studentWhitespaceData())
	if _, _, err := s.AddStudent(" \t　 "); err == nil {
		t.Fatal("混合空白编号应被拒绝")
	}
	if got := len(s.Students()); got != 1 {
		t.Fatalf("拒绝空白编号不应改变学生数量，得到 %d", got)
	}
	if st, a, err := s.AddStudent(" s1 "); err != nil || a != ActionCreated || st.ID != " s1 " {
		t.Fatalf("拒绝空白后登记含实际文字的编号应正常新建，%+v action=%v err=%v",
			st, a, err)
	}
}
