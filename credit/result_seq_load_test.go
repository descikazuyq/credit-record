package credit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件为“读取已有成绩记录时，两次独立的成绩提交不能共用同一个结果提交
// 序号”这项既有行为提供回归保障。该冲突无法经正常提交流程落入文件（每次
// 提交都从递增计数器取号），因此全部用例直接构造记录文件，聚焦 Load 读取
// 历史这一路径：
//   - 文件完整可解析、学生/课程/要求齐全、修读归属与结果状态合法，只要两条
//     不同的修读（都已提交结果）共用同一个正整数序号，就整份判为损坏：
//     无论两条都通过还是一条未通过，无论它们是同一学生的重复修读、同一学生
//     不同要求的独立提交，还是不同学生的独立修读；即使文件里另有完全正常的
//     学生，也不能只读入正常部分继续核对；
//   - 修读编号相同、学期相同、记录在文件里的排列顺序，都不是冲突依据；
//   - 合法记录中两次通过按“提交序号较小者”说明来源，与文件排列、修读编号、
//     学期先后都无关；更早提交的未通过保留在历史中但不成为来源；
//   - 只有选课、尚未提交结果的修读一律使用序号 0，多条并存合法且不得学分；
//     已提交序号允许有间隔；不同学生共用修读编号但序号不同时各自保留归属。
//
// 所有合法用例都断言只读核对不改动原文件。

// seqBaseRecord 构造结构完整、引用齐全的基础记录：学生 s1 的要求 r1 指向
// 4 学分课程 c1。修读与序号计数器由各用例自行补充。
func seqBaseRecord() *fileData {
	return &fileData{
		Version:      recordVersion,
		Courses:      []*Course{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
	}
}

// seqEnr 构造一条修读记录；result 为 enrolled 时 seq 传 0。
func seqEnr(id, student, req, term string, result Result, seq int) *Enrollment {
	return &Enrollment{
		ID: id, StudentID: student, ReqID: req, Term: term,
		Result: result, ResultSeq: seq,
	}
}

// assertDupSeqRejected 是序号冲突用例的共同断言：Load 必须失败、文件标记为
// 已存在，错误点名问题文件、说明内容损坏并给出重复的提交序号，且原文件
// 字节完整保留、不会只加载其中一部分记录。
func assertDupSeqRejected(t *testing.T, d *fileData, seq int) {
	t.Helper()
	path, raw := writeRecord(t, d)
	s, existed, err := Load(path)
	if err == nil {
		t.Fatalf("两条已提交修读共用序号 %d 时必须整份拒绝，却得到 store=%v", seq, s)
	}
	if s != nil {
		t.Fatalf("序号冲突损坏整份记录时不得返回部分记录，得到 %v", s)
	}
	if !existed {
		t.Fatalf("冲突记录应标记为文件已存在，existed=%v err=%v", existed, err)
	}
	msg := err.Error()
	for _, want := range []string{
		path,
		"内容损坏",
		fmt.Sprintf("结果提交序号 %d 重复", seq),
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误信息应包含 %q（点名问题文件与重复序号），得到：%v", want, err)
		}
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(raw) {
		t.Fatalf("拒绝读取不得改写原文件\nwant=%q\n got=%q", raw, got)
	}
}

// TestLoadRejectsDuplicateResultSeq 文件完整、引用齐全、归属与结果合法，
// 但两条不同的已提交修读共用同一个正整数序号时，无论两条结果如何、归属
// 如何、排列如何，都必须整份判为损坏并保留原文件。
func TestLoadRejectsDuplicateResultSeq(t *testing.T) {
	t.Run("两条通过_同一学生同一学期重复修读", func(t *testing.T) {
		// 同一学期重复修读本身合法（编号不同即可），但共用序号仍是损坏。
		d := seqBaseRecord()
		d.Enrollments = []*Enrollment{
			seqEnr("e1", "s1", "r1", "2024春", Passed, 7),
			seqEnr("e2", "s1", "r1", "2024春", Passed, 7),
		}
		d.NextResultSeq = 7
		assertDupSeqRejected(t, d, 7)
	})

	t.Run("一条通过一条未通过", func(t *testing.T) {
		// 未通过同样占用一次提交序号：一条通过、一条未通过共用序号也损坏。
		d := seqBaseRecord()
		d.Enrollments = []*Enrollment{
			seqEnr("e1", "s1", "r1", "2024春", Passed, 5),
			seqEnr("e2", "s1", "r1", "2024秋", Failed, 5),
		}
		d.NextResultSeq = 5
		assertDupSeqRejected(t, d, 5)
	})

	t.Run("同一学生不同要求的独立提交", func(t *testing.T) {
		// 序号刻画的是两次独立提交的先后关系，不按要求划分：不同要求下的
		// 两次提交共用序号同样损坏。
		d := seqBaseRecord()
		d.Courses = append(d.Courses, &Course{ID: "c2", Name: "大学物理", Credit: 3, Open: true})
		d.Requirements = append(d.Requirements, &Requirement{ID: "r2", StudentID: "s1", CourseID: "c2"})
		d.Enrollments = []*Enrollment{
			seqEnr("e1", "s1", "r1", "2024春", Passed, 4),
			seqEnr("e9", "s1", "r2", "2024春", Passed, 4),
		}
		d.NextResultSeq = 4
		assertDupSeqRejected(t, d, 4)
	})

	t.Run("不同学生的独立修读_同编号同学期", func(t *testing.T) {
		// 修读编号、学期在学生之间相同都合法；唯一不能共用的是提交序号。
		d := seqBaseRecord()
		d.Students = append(d.Students, &Student{ID: "s2"})
		d.Requirements = append(d.Requirements, &Requirement{ID: "r1", StudentID: "s2", CourseID: "c1"})
		d.Enrollments = []*Enrollment{
			seqEnr("e1", "s1", "r1", "2024春", Passed, 3),
			seqEnr("e1", "s2", "r1", "2024春", Failed, 3),
		}
		d.NextResultSeq = 3
		assertDupSeqRejected(t, d, 3)
	})

	t.Run("重复序号不相邻_夹杂其他提交", func(t *testing.T) {
		// 文件排列顺序不是判定依据：两条同序号记录之间夹着另一条合法提交，
		// 仍要揪出重复，不能只比较相邻记录或按读取顺序放过。
		d := seqBaseRecord()
		d.Enrollments = []*Enrollment{
			seqEnr("e1", "s1", "r1", "2024春", Passed, 6),
			seqEnr("e3", "s1", "r1", "2025春", Failed, 8),
			seqEnr("e2", "s1", "r1", "2024秋", Passed, 6),
		}
		d.NextResultSeq = 8
		assertDupSeqRejected(t, d, 6)
	})

	t.Run("另有完全正常的学生仍整份拒绝", func(t *testing.T) {
		// s2 的课程、要求、修读与序号全部合法：s1 名下的序号冲突仍使整份
		// 文件不可读，不能只读入 s2 继续核对。
		d := seqBaseRecord()
		d.Courses = append(d.Courses, &Course{ID: "c2", Name: "线性代数", Credit: 3, Open: true})
		d.Students = append(d.Students, &Student{ID: "s2"})
		d.Requirements = append(d.Requirements, &Requirement{ID: "r1", StudentID: "s2", CourseID: "c2"})
		d.Enrollments = []*Enrollment{
			seqEnr("e1", "s1", "r1", "2024春", Passed, 2),
			seqEnr("e2", "s1", "r1", "2024秋", Failed, 2),
			seqEnr("e1", "s2", "r1", "2024春", Passed, 4),
		}
		d.NextResultSeq = 4
		path, raw := writeRecord(t, d)
		s, existed, err := Load(path)
		if err == nil {
			t.Fatalf("s1 的序号冲突应让整份文件不可读，却得到 store=%v", s)
		}
		if s != nil || !existed {
			t.Fatalf("不得返回 s2 的部分记录，s=%v existed=%v", s, existed)
		}
		if !strings.Contains(err.Error(), "结果提交序号 2 重复") {
			t.Fatalf("冲突应归属 s1 重复的序号 2，得到：%v", err)
		}
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != string(raw) {
			t.Fatal("整份拒绝时不得改写原文件")
		}
	})
}

// loadLegalRecord 加载合法记录：必须成功、记录标记为已存在，且只读核对
// 不把记录标记为变更、不改写原文件；返回加载后的记录集。
func loadLegalRecord(t *testing.T, d *fileData) *Store {
	t.Helper()
	path, raw := writeRecord(t, d)
	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("合法记录应正常读取，existed=%v err=%v", existed, err)
	}
	_ = s.CheckStudent("s1") // 只读核对本身不得改动记录。
	if s.Dirty() {
		t.Fatal("只读核对不应把记录标记为已变更")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(raw) {
		t.Fatalf("读取与核对不得改动合法记录文件\nwant=%q\n got=%q", raw, got)
	}
	return s
}

// TestLoadReorderedPassesPickEarliestSeqSource 一项 4 学分要求有两次通过
// 修读，文件把后提交的记录排在前面，且修读编号与学期的先后都与提交先后
// 相反：没有有效免修时仍以提交序号较小的通过修读说明来源，两次通过都保留，
// 要求满足、总学分仍是 4。
func TestLoadReorderedPassesPickEarliestSeqSource(t *testing.T) {
	// 文件顺序 [e1(seq2, 2024春), e2(seq1, 2024秋)]：
	// 按文件排列、编号升序、学期升序都会挑到 e1，只有提交序号指向 e2。
	d := seqBaseRecord()
	d.Enrollments = []*Enrollment{
		seqEnr("e1", "s1", "r1", "2024春", Passed, 2),
		seqEnr("e2", "s1", "r1", "2024秋", Passed, 1),
	}
	d.NextResultSeq = 2
	path, _ := writeRecord(t, d)
	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("序号各不相同的合法记录应正常读取，existed=%v err=%v", existed, err)
	}
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
		t.Fatalf("要求应满足且总学分仍是 4，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	st := rep.Requirements[0]
	if !st.Satisfied || st.Source != "enrollment" || st.PassedEnrollmentID != "e2" {
		t.Fatalf("应以提交序号较小的通过修读 e2 说明来源，不能按文件排列/编号/学期挑选，得到 %+v", st)
	}
	if len(st.PassedEnrollmentIDs) != 2 ||
		st.PassedEnrollmentIDs[0] != "e2" || st.PassedEnrollmentIDs[1] != "e1" {
		t.Fatalf("两次通过记录都应按提交先后保留，得到 %v", st.PassedEnrollmentIDs)
	}
	if s.Dirty() {
		t.Fatal("只读核对不应把记录标记为已变更")
	}

	// 两条记录各自的结果、学期与序号保持原样。
	e1 := s.Enrollment("s1", "e1")
	e2 := s.Enrollment("s1", "e2")
	if e1 == nil || e1.Result != Passed || e1.ResultSeq != 2 || e1.Term != "2024春" {
		t.Fatalf("e1 应原样保留，得到 %+v", e1)
	}
	if e2 == nil || e2.Result != Passed || e2.ResultSeq != 1 || e2.Term != "2024秋" {
		t.Fatalf("e2 应原样保留，得到 %+v", e2)
	}

	// 用现有格式重新保存再加载：来源仍是序号最小的 e2，结论不漂移。
	path2 := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path2); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, existed, err := Load(path2)
	if err != nil || !existed {
		t.Fatalf("重新保存后应可再次加载，existed=%v err=%v", existed, err)
	}
	rep2 := reloaded.CheckStudent("s1")
	if rep2.TotalCredits != 4 || rep2.Requirements[0].PassedEnrollmentID != "e2" ||
		len(rep2.Requirements[0].PassedEnrollmentIDs) != 2 {
		t.Fatalf("重新加载后来源仍应是 e2、两次通过都保留，得到 %+v", rep2)
	}
}

// TestLoadEarlierFailedSubmissionStaysHistoryNotSource 存在比两次通过更早
// 提交的未通过修读时，它必须保留在修读历史中（结果与序号不变），但不能
// 取代序号最小的通过修读成为学分来源；总学分仍是一份 4 学分。
func TestLoadEarlierFailedSubmissionStaysHistoryNotSource(t *testing.T) {
	// 文件排列刻意打乱：e1 未通过（序号 1、学期反而更晚），两条通过分别
	// 是序号 3 的 e2 与序号 2 的 e3。来源应是通过修读中序号最小的 e3。
	d := seqBaseRecord()
	d.Enrollments = []*Enrollment{
		seqEnr("e2", "s1", "r1", "2024秋", Passed, 3),
		seqEnr("e1", "s1", "r1", "2025春", Failed, 1),
		seqEnr("e3", "s1", "r1", "2024春", Passed, 2),
	}
	d.NextResultSeq = 3
	s := loadLegalRecord(t, d)

	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
		t.Fatalf("有通过修读时要求应满足、总学分 4，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	st := rep.Requirements[0]
	if st.Source != "enrollment" || st.PassedEnrollmentID != "e3" {
		t.Fatalf("来源应是通过修读中序号最小的 e3，未通过不能成为来源，得到 %+v", st)
	}
	if len(st.PassedEnrollmentIDs) != 2 ||
		st.PassedEnrollmentIDs[0] != "e3" || st.PassedEnrollmentIDs[1] != "e2" {
		t.Fatalf("通过历史应只含两次通过并按序号排列，得到 %v", st.PassedEnrollmentIDs)
	}

	// 更早提交的未通过记录保留在历史中，结果与序号不变。
	failed := s.Enrollment("s1", "e1")
	if failed == nil || failed.Result != Failed || failed.ResultSeq != 1 || failed.Term != "2025春" {
		t.Fatalf("更早提交的未通过修读应原样保留在历史中，得到 %+v", failed)
	}
	if n := len(s.Enrollments("s1")); n != 3 {
		t.Fatalf("三条修读记录都应保留，得到 %d 条", n)
	}
}

// TestLoadMultipleEnrolledZeroSeqLegalAndWorthNoCredit 只有选课、尚未提交
// 结果的修读统一使用序号 0：多条这样的修读同时存在（含同一学期重复修读、
// 跨学生并存）是合法记录，不能因共享 0 而报序号重复，也不能由此获得学分；
// 与已提交记录混放时结论也不变。
func TestLoadMultipleEnrolledZeroSeqLegalAndWorthNoCredit(t *testing.T) {
	t.Run("只有多条选课", func(t *testing.T) {
		d := seqBaseRecord()
		d.Enrollments = []*Enrollment{
			seqEnr("e1", "s1", "r1", "2024春", Enrolled, 0),
			seqEnr("e2", "s1", "r1", "2024春", Enrolled, 0), // 同一学期重复修读
			seqEnr("e3", "s1", "r1", "2024秋", Enrolled, 0),
		}
		d.NextResultSeq = 0
		s := loadLegalRecord(t, d)
		rep := s.CheckStudent("s1")
		if rep.TotalCredits != 0 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
			t.Fatalf("仅选课不应获得学分，r1 应未满足，得到学分=%d 未满足=%v",
				rep.TotalCredits, rep.Unmet)
		}
		st := rep.Requirements[0]
		if st.Satisfied || st.Source != "" || len(st.PassedEnrollmentIDs) != 0 {
			t.Fatalf("选课记录不能成为来源或进入通过历史，得到 %+v", st)
		}
		if n := len(s.Enrollments("s1")); n != 3 {
			t.Fatalf("三条选课记录都应保留，得到 %d 条", n)
		}
	})

	t.Run("多条选课与已提交记录及另一名学生并存", func(t *testing.T) {
		d := seqBaseRecord()
		d.Students = append(d.Students, &Student{ID: "s2"})
		d.Requirements = append(d.Requirements, &Requirement{ID: "r1", StudentID: "s2", CourseID: "c1"})
		d.Enrollments = []*Enrollment{
			seqEnr("e1", "s1", "r1", "2024春", Enrolled, 0),
			seqEnr("e2", "s1", "r1", "2024春", Enrolled, 0),
			seqEnr("e3", "s1", "r1", "2024秋", Passed, 1),
			seqEnr("e1", "s2", "r1", "2024春", Enrolled, 0), // 跨学生共享编号与序号 0
		}
		d.NextResultSeq = 1
		s := loadLegalRecord(t, d)

		rep := s.CheckStudent("s1")
		if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
			t.Fatalf("s1 应只凭通过的 e3 获得 4 学分，得到学分=%d 未满足=%v",
				rep.TotalCredits, rep.Unmet)
		}
		st := rep.Requirements[0]
		if st.PassedEnrollmentID != "e3" || len(st.PassedEnrollmentIDs) != 1 {
			t.Fatalf("来源应只有 e3，选课记录不得计入，得到 %+v", st)
		}
		// s2 的同号选课仍是选课、0 学分，不与 s1 的任何记录冲突。
		rep2 := s.CheckStudent("s2")
		if rep2.TotalCredits != 0 || len(rep2.Unmet) != 1 || rep2.Unmet[0] != "r1" {
			t.Fatalf("s2 的选课应保持 0 学分未满足，得到 %+v", rep2)
		}
		if e := s.Enrollment("s2", "e1"); e == nil || e.Result != Enrolled || e.ResultSeq != 0 {
			t.Fatalf("s2 名下应保留本人的选课 e1，得到 %+v", e)
		}
	})
}

// TestLoadSubmittedSeqGapsNeedNotBeConsecutive 已提交序号允许有间隔，不要求
// 连续编号：跳号的合法记录正常读取，来源仍是通过记录中序号最小的一条，
// 未通过不成为来源，选课记录不得携带序号。
func TestLoadSubmittedSeqGapsNeedNotBeConsecutive(t *testing.T) {
	d := seqBaseRecord()
	d.Enrollments = []*Enrollment{
		seqEnr("e1", "s1", "r1", "2024春", Failed, 1),
		seqEnr("e2", "s1", "r1", "2024夏", Passed, 5), // 序号 2/3/4 缺失
		seqEnr("e3", "s1", "r1", "2024秋", Passed, 9), // 继续跳号
		seqEnr("e4", "s1", "r1", "2025春", Enrolled, 0),
	}
	d.NextResultSeq = 9
	s := loadLegalRecord(t, d)

	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
		t.Fatalf("跳号记录应正常核对到 4 学分，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	st := rep.Requirements[0]
	if st.Source != "enrollment" || st.PassedEnrollmentID != "e2" {
		t.Fatalf("序号 1 的未通过不成为来源，应取通过中序号最小的 e2，得到 %+v", st)
	}
	if len(st.PassedEnrollmentIDs) != 2 ||
		st.PassedEnrollmentIDs[0] != "e2" || st.PassedEnrollmentIDs[1] != "e3" {
		t.Fatalf("两次通过应按序号保留为 [e2 e3]，得到 %v", st.PassedEnrollmentIDs)
	}
	if e := s.Enrollment("s1", "e4"); e == nil || e.Result != Enrolled || e.ResultSeq != 0 {
		t.Fatalf("选课记录 e4 应保留且无序号，得到 %+v", e)
	}
}

// TestLoadSameEnrollmentIDAcrossStudentsDifferentSeq 不同学生可以共用修读
// 编号（学期也可相同），只要提交序号不同就各自保留所属记录：结果、序号、
// 学分来源各归本人，不能因编号或学期相同误报冲突或串记录。
func TestLoadSameEnrollmentIDAcrossStudentsDifferentSeq(t *testing.T) {
	t.Run("一通过一未通过", func(t *testing.T) {
		d := seqBaseRecord()
		d.Courses = append(d.Courses, &Course{ID: "c2", Name: "线性代数", Credit: 3, Open: true})
		d.Students = append(d.Students, &Student{ID: "s2"})
		d.Requirements = append(d.Requirements, &Requirement{ID: "r1", StudentID: "s2", CourseID: "c2"})
		d.Enrollments = []*Enrollment{
			seqEnr("e1", "s1", "r1", "2024春", Passed, 1),
			seqEnr("e1", "s2", "r1", "2024春", Failed, 2),
		}
		d.NextResultSeq = 2
		path, raw := writeRecord(t, d)
		s, existed, err := Load(path)
		if err != nil || !existed {
			t.Fatalf("跨学生同编号、序号不同应正常读取，existed=%v err=%v", existed, err)
		}

		// s1：本人 e1 通过，4 学分，来源本人 e1。
		rep1 := s.CheckStudent("s1")
		if rep1.TotalCredits != 4 || len(rep1.Unmet) != 0 {
			t.Fatalf("s1 应凭本人通过修读得 4 学分，得到学分=%d 未满足=%v",
				rep1.TotalCredits, rep1.Unmet)
		}
		if st := rep1.Requirements[0]; st.Source != "enrollment" ||
			st.PassedEnrollmentID != "e1" || st.Course.Credit != 4 {
			t.Fatalf("s1 的来源应是本人 4 学分课程的 e1，得到 %+v", st)
		}
		// s2：本人 e1 未通过，0 学分，要求未满足。
		rep2 := s.CheckStudent("s2")
		if rep2.TotalCredits != 0 || len(rep2.Unmet) != 1 || rep2.Unmet[0] != "r1" {
			t.Fatalf("s2 本人的 e1 未通过，应 0 学分未满足，得到 %+v", rep2)
		}
		if len(rep2.Requirements[0].PassedEnrollmentIDs) != 0 {
			t.Fatalf("s2 不应有通过历史，得到 %+v", rep2.Requirements[0])
		}

		// 同号修读是两条独立记录，结果与序号各归本人。
		e1s1 := s.Enrollment("s1", "e1")
		e1s2 := s.Enrollment("s2", "e1")
		if e1s1 == nil || e1s2 == nil || e1s1 == e1s2 {
			t.Fatalf("两名学生的同号修读应各自独立，得到 %p %p", e1s1, e1s2)
		}
		if e1s1.Result != Passed || e1s1.ResultSeq != 1 {
			t.Fatalf("s1 的 e1 应保留通过、序号 1，得到 %+v", e1s1)
		}
		if e1s2.Result != Failed || e1s2.ResultSeq != 2 {
			t.Fatalf("s2 的 e1 应保留未通过、序号 2，得到 %+v", e1s2)
		}

		// 两人的只读核对都不得改动原文件。
		_ = s.CheckStudent("s1")
		_ = s.CheckStudent("s2")
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != string(raw) {
			t.Fatal("只读核对不得改写跨学生的合法记录")
		}
	})

	t.Run("两人都通过_序号不同", func(t *testing.T) {
		d := seqBaseRecord()
		d.Courses = append(d.Courses, &Course{ID: "c2", Name: "线性代数", Credit: 3, Open: true})
		d.Students = append(d.Students, &Student{ID: "s2"})
		d.Requirements = append(d.Requirements, &Requirement{ID: "r1", StudentID: "s2", CourseID: "c2"})
		d.Enrollments = []*Enrollment{
			// 后提交的 s2 记录排在文件前面，证明来源只看序号、不看排列。
			seqEnr("e1", "s2", "r1", "2024春", Passed, 2),
			seqEnr("e1", "s1", "r1", "2024春", Passed, 1),
		}
		d.NextResultSeq = 2
		s := loadLegalRecord(t, d)

		rep1 := s.CheckStudent("s1")
		if rep1.TotalCredits != 4 || rep1.Requirements[0].PassedEnrollmentID != "e1" {
			t.Fatalf("s1 应凭本人 e1 得 4 学分，得到 %+v", rep1)
		}
		rep2 := s.CheckStudent("s2")
		if rep2.TotalCredits != 3 || rep2.Requirements[0].PassedEnrollmentID != "e1" {
			t.Fatalf("s2 应凭本人 e1（另一门 3 学分课程）得 3 学分，得到 %+v", rep2)
		}
		// 两条同号通过各只属于本人，不能互相进入对方的通过历史。
		if len(rep1.Requirements[0].PassedEnrollmentIDs) != 1 ||
			len(rep2.Requirements[0].PassedEnrollmentIDs) != 1 {
			t.Fatalf("两人的通过历史都应只有本人的一条，得到 %v / %v",
				rep1.Requirements[0].PassedEnrollmentIDs,
				rep2.Requirements[0].PassedEnrollmentIDs)
		}
	})
}
