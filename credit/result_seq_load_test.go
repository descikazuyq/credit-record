package credit

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// 本文件为“读取已有记录时，两次独立的成绩提交不能共用同一个结果提交序号”
// 这项既有行为提供回归保障。序号是核对时挑选“最先提交的通过修读”的唯一
// 依据，冲突无法经正常提交流程落入文件（序号由计数器单调分配），因此全部
// 用例直接构造记录文件，聚焦 Load 读取历史这一路径：
//   - 文件完整可解析，学生、课程、要求都存在，修读归属与结果状态均合法，
//     只要两条已提交结果（通过/未通过皆然）共用同一个正整数序号，就整份
//     记录损坏：同一学生的重复修读如此，不同学生各自的独立修读同样如此；
//     修读编号相同、学期相同或记录在文件里的排列顺序都不能当作“同一次
//     提交”而放行，另一名学生完全正常的记录也不能让文件被部分读入；
//   - 只有选课、尚未提交成绩的修读使用序号 0，多条并存合法、不得学分；
//   - 已提交序号允许有间隔；不同学生共用修读编号但序号不同，各自保留；
//   - 合法记录的核对继续沿用现有规则：没有有效免修时以序号最小的通过
//     修读说明来源，文件排列顺序、修读编号与学期次序都不能改变挑选结果，
//     更早提交的未通过修读保留在历史中但不成为来源。

// seqBaseRecord 构造一份结构完整、引用齐全的基础记录：一门 4 学分课程 c1，
// 学生 s1（可选再加入 s2 与第二门课程），每人的要求 r1 指向各自课程。
func seqBaseRecord(t *testing.T, withSecondStudent bool) *fileData {
	t.Helper()
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
		},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
	}
	if withSecondStudent {
		d.Courses = append(d.Courses, &Course{ID: "c2", Name: "线性代数", Credit: 3, Open: true})
		d.Students = append(d.Students, &Student{ID: "s2"})
		d.Requirements = append(d.Requirements,
			&Requirement{ID: "r1", StudentID: "s2", CourseID: "c2"})
	}
	return d
}

// seqEnr 构造一条属于 student 的 r1、编号 id、学期 term 的修读记录。
func seqEnr(student, id, term string, result Result, seq int) *Enrollment {
	return &Enrollment{
		ID: id, StudentID: student, ReqID: "r1", Term: term,
		Result: result, ResultSeq: seq,
	}
}

// assertDuplicateSeqRejected 是序号冲突用例的共同断言：Load 必须失败、
// 文件标记为已存在，错误点名问题文件与重复的提交序号并说明内容损坏，
// 且原文件字节完整保留，绝不返回可继续核对的部分记录。
func assertDuplicateSeqRejected(t *testing.T, path string, raw []byte, seq int, label string) {
	t.Helper()
	s, existed, err := Load(path)
	if err == nil {
		t.Fatalf("%s：共用结果提交序号 %d 时必须整份拒绝，却得到 store=%v", label, seq, s)
	}
	if !existed {
		t.Fatalf("%s：冲突记录应标记为文件已存在，existed=%v err=%v", label, existed, err)
	}
	msg := err.Error()
	for _, want := range []string{path, "内容损坏", "结果提交序号", "重复"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("%s：错误信息应包含 %q，得到：%v", label, want, err)
		}
	}
	if !strings.Contains(msg, strconv.Itoa(seq)) {
		t.Fatalf("%s：错误信息应指出重复的提交序号 %d，得到：%v", label, seq, err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(raw) {
		t.Fatalf("%s：拒绝读取不得改写原文件\nwant=%q\n got=%q", label, raw, got)
	}
}

// TestLoadRejectsDuplicateResultSeq 文件完整可解析、其余内容全部合法，
// 仅因两条已提交修读共用同一个正整数结果提交序号就必须判为整份损坏：
// 无论两条都通过还是一条未通过、无论属于同一学生的重复修读还是不同学生
// 的独立修读、无论两条在文件中怎样排列或是否相邻、也无论另一名学生的
// 记录是否完全正常。
func TestLoadRejectsDuplicateResultSeq(t *testing.T) {
	t.Run("同一学生重复修读_两条都通过", func(t *testing.T) {
		d := seqBaseRecord(t, false)
		d.Enrollments = []*Enrollment{
			seqEnr("s1", "e1", "2024春", Passed, 1),
			seqEnr("s1", "e2", "2024春", Passed, 1),
		}
		d.NextResultSeq = 1
		path, raw := writeRecord(t, d)
		assertDuplicateSeqRejected(t, path, raw, 1, "同一学期两条通过")
	})

	t.Run("同一学生_一通过一未通过", func(t *testing.T) {
		d := seqBaseRecord(t, false)
		d.Enrollments = []*Enrollment{
			seqEnr("s1", "e1", "2024春", Passed, 2),
			seqEnr("s1", "e2", "2024秋", Failed, 2),
		}
		d.NextResultSeq = 2
		path, raw := writeRecord(t, d)
		assertDuplicateSeqRejected(t, path, raw, 2, "通过与未通过共用序号")
	})

	t.Run("同一学生_一未通过一通过且文件次序相反", func(t *testing.T) {
		// 交换两条记录的文件排列：未通过排在前面，且两条编号、学期均不同，
		// 仍只按序号判定冲突。
		d := seqBaseRecord(t, false)
		d.Enrollments = []*Enrollment{
			seqEnr("s1", "e9", "2025春", Failed, 7),
			seqEnr("s1", "e1", "2024春", Passed, 7),
		}
		d.NextResultSeq = 7
		path, raw := writeRecord(t, d)
		assertDuplicateSeqRejected(t, path, raw, 7, "文件次序相反的通过/未通过")
	})

	t.Run("不同学生独立修读_两条都通过", func(t *testing.T) {
		// 两名学生可以使用相同的修读编号、相同的学期，但提交序号是全局的
		// 先后关系：序号相同就不是两次独立提交，必须整份损坏。
		d := seqBaseRecord(t, true)
		d.Enrollments = []*Enrollment{
			seqEnr("s1", "e1", "2024春", Passed, 3),
			seqEnr("s2", "e1", "2024春", Passed, 3),
		}
		d.NextResultSeq = 3
		path, raw := writeRecord(t, d)
		assertDuplicateSeqRejected(t, path, raw, 3, "跨学生同编号同学期两条通过")
	})

	t.Run("不同学生_一通过一未通过", func(t *testing.T) {
		d := seqBaseRecord(t, true)
		d.Enrollments = []*Enrollment{
			seqEnr("s1", "e1", "2024春", Failed, 4),
			seqEnr("s2", "e1", "2024秋", Passed, 4),
		}
		d.NextResultSeq = 4
		path, raw := writeRecord(t, d)
		assertDuplicateSeqRejected(t, path, raw, 4, "跨学生通过与未通过共用序号")
	})

	t.Run("冲突记录不相邻_选课与其他学生记录夹在中间", func(t *testing.T) {
		d := seqBaseRecord(t, true)
		d.Enrollments = []*Enrollment{
			seqEnr("s1", "e1", "2024春", Passed, 5),
			seqEnr("s1", "e2", "2024秋", Enrolled, 0), // 只有选课，序号 0
			seqEnr("s2", "e2", "2024秋", Passed, 6),
			seqEnr("s1", "e3", "2025春", Failed, 5), // 与 e1 共用序号 5，不相邻
			seqEnr("s2", "e3", "2025春", Enrolled, 0),
		}
		d.NextResultSeq = 6
		path, raw := writeRecord(t, d)
		assertDuplicateSeqRejected(t, path, raw, 5, "冲突记录不相邻")
	})

	t.Run("无关学生的合法记录不能掩盖冲突", func(t *testing.T) {
		// s2 的课程、要求与一次通过修读（独立序号 8）完全合法；s1 名下
		// 的序号冲突仍要整份拒绝，不能只读入 s2 的正常部分继续核对。
		d := seqBaseRecord(t, true)
		d.Enrollments = []*Enrollment{
			seqEnr("s1", "e1", "2024春", Passed, 2),
			seqEnr("s1", "e2", "2024秋", Failed, 2),
			seqEnr("s2", "e1", "2024春", Passed, 8),
		}
		d.NextResultSeq = 8
		path, raw := writeRecord(t, d)
		s, existed, err := Load(path)
		if err == nil {
			t.Fatalf("s1 的序号冲突应让整份文件不可读，不能只加载 s2，得到 store=%v", s)
		}
		if !existed {
			t.Fatalf("冲突记录应标记为文件已存在，existed=%v", existed)
		}
		if !strings.Contains(err.Error(), strconv.Itoa(2)) {
			t.Fatalf("错误应指出重复的序号 2（而非 s2 的合法序号 8），得到：%v", err)
		}
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != string(raw) {
			t.Fatal("拒绝读取不得改写原文件")
		}
	})
}

// TestLoadLegalResultSeqRecordsCheckNormally 合法序号记录的读取与核对：
// 序号各不相同时，文件排列顺序、修读编号大小与学期次序都不能影响“最先
// 提交的通过修读”的挑选；更早提交的未通过修读保留在历史中但不成为来源；
// 序号允许有间隔。同一份记录换一种文件排列，核对结论必须完全一致。
func TestLoadLegalResultSeqRecordsCheckNormally(t *testing.T) {
	// 主情形（文件排列刻意与提交先后、编号、学期全部不一致）：
	//   - e2（编号较大、学期较晚）提交序号 2，是最先提交的通过修读；
	//   - e1（编号较小、学期较早）提交序号 5，后提交的通过修读；
	//   - e3（学期最早）提交序号 1，是更早提交的未通过修读；
	// 文件里先放 e1，再放 e3，最后放 e2；序号在 2 和 5 之间留有间隔。
	build := func(fileOrder []string) *fileData {
		byID := map[string]*Enrollment{
			"e1": seqEnr("s1", "e1", "2024春", Passed, 5),
			"e2": seqEnr("s1", "e2", "2025秋", Passed, 2),
			"e3": seqEnr("s1", "e3", "2023秋", Failed, 1),
			// 两条只有选课、尚未提交成绩的修读（含一条显式写出的序号 0）。
			"e4": seqEnr("s1", "e4", "2024春", Enrolled, 0),
			"e5": {ID: "e5", StudentID: "s1", ReqID: "r1", Term: "2024秋",
				Result: Enrolled},
		}
		d := seqBaseRecord(t, false)
		for _, id := range fileOrder {
			d.Enrollments = append(d.Enrollments, byID[id])
		}
		d.NextResultSeq = 5
		return d
	}

	assertMainScenario := func(t *testing.T, s *Store) {
		t.Helper()
		rep := s.CheckStudent("s1")
		if rep.TotalCredits != 4 {
			t.Fatalf("两次通过只计一份 4 学分，得到 %d", rep.TotalCredits)
		}
		if len(rep.Unmet) != 0 {
			t.Fatalf("要求 r1 应已满足，未满足=%v", rep.Unmet)
		}
		st := rep.Requirements[0]
		if !st.Satisfied || st.Source != "enrollment" {
			t.Fatalf("没有有效免修时应由通过修读满足，得到 %+v", st)
		}
		if st.PassedEnrollmentID != "e2" {
			t.Fatalf("来源必须是提交序号最小的通过修读 e2，"+
				"不能按文件次序/编号/学期挑成 e1，得到 %s", st.PassedEnrollmentID)
		}
		// 两次通过记录都按提交先后保留在修读历史中。
		if len(st.PassedEnrollmentIDs) != 2 ||
			st.PassedEnrollmentIDs[0] != "e2" || st.PassedEnrollmentIDs[1] != "e1" {
			t.Fatalf("两次通过记录应按提交序号保留为 [e2 e1]，得到 %v",
				st.PassedEnrollmentIDs)
		}
		// 更早提交的未通过修读保留在历史中，但不是来源、不产生学分。
		if e := s.Enrollment("s1", "e3"); e == nil || e.Result != Failed || e.ResultSeq != 1 {
			t.Fatalf("更早的未通过修读 e3 应原样保留，得到 %+v", e)
		}
		// 只有选课的修读保留且不获得学分。
		for _, id := range []string{"e4", "e5"} {
			if e := s.Enrollment("s1", id); e == nil || e.Result != Enrolled || e.ResultSeq != 0 {
				t.Fatalf("选课修读 %s 应保留为选课且序号为 0，得到 %+v", id, e)
			}
		}
	}

	for name, order := range map[string][]string{
		"文件次序与提交先后不一致": {"e1", "e3", "e4", "e2", "e5"},
		"按文件倒序再读一次":    {"e5", "e2", "e4", "e3", "e1"},
	} {
		t.Run(name, func(t *testing.T) {
			path, raw := writeRecord(t, build(order))
			s, existed, err := Load(path)
			if err != nil || !existed {
				t.Fatalf("序号合法的记录应正常读取，existed=%v err=%v", existed, err)
			}
			assertMainScenario(t, s)

			// 只读核对不改文件，也不把记录标记为已变更。
			if s.Dirty() {
				t.Fatal("只读加载的记录不应被标记为已变更")
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != string(raw) {
				t.Fatal("读取与核对不得改动原文件")
			}

			// 重新保存后再次加载：来源、历史与学分完全保持。
			path2 := path + ".saved"
			if err := s.Save(path2); err != nil {
				t.Fatalf("Save: %v", err)
			}
			reloaded, existed2, err := Load(path2)
			if err != nil || !existed2 {
				t.Fatalf("重新保存后应可再次加载，existed=%v err=%v", existed2, err)
			}
			assertMainScenario(t, reloaded)
		})
	}
}

// TestLoadMultipleEnrolledSharingSeqZero 只有选课、尚未提交成绩的修读使用
// 序号 0：同一学生同一学期的多条选课、不同学生的多条选课同时存在都合法，
// 不能因共享 0 报重复，也不能由此获得学分；它们与真正提交过的结果并存时
// 核对结论不变。
func TestLoadMultipleEnrolledSharingSeqZero(t *testing.T) {
	d := seqBaseRecord(t, true)
	d.Enrollments = []*Enrollment{
		// s1 同一学期两条选课（编号不同），都没有 resultSeq（读入为 0）。
		seqEnr("s1", "e1", "2024春", Enrolled, 0),
		{ID: "e2", StudentID: "s1", ReqID: "r1", Term: "2024春", Result: Enrolled},
		// s2 的同编号、同学期选课：跨学生共享编号与序号 0 同样合法。
		seqEnr("s2", "e1", "2024春", Enrolled, 0),
		// s2 另有一条真正通过的修读，序号 1；它不能被选课的 0 影响。
		seqEnr("s2", "e2", "2024秋", Passed, 1),
	}
	d.NextResultSeq = 1
	path, raw := writeRecord(t, d)

	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("多条选课共享序号 0 应正常读取，existed=%v err=%v", existed, err)
	}

	// s1：只有选课，要求未满足、0 学分。
	rep1 := s.CheckStudent("s1")
	if rep1.TotalCredits != 0 || len(rep1.Unmet) != 1 || rep1.Unmet[0] != "r1" {
		t.Fatalf("s1 只有选课，应 0 学分且要求未满足，得到 %+v", rep1)
	}
	if st := rep1.Requirements[0]; st.Satisfied || st.Source != "" ||
		len(st.PassedEnrollmentIDs) != 0 {
		t.Fatalf("选课不能成为来源或进入通过历史，得到 %+v", st)
	}
	if n := len(s.Enrollments("s1")); n != 2 {
		t.Fatalf("s1 的两条选课都应保留，得到 %d 条", n)
	}

	// s2：一条选课不影响另一条通过，仍只得自己的 3 学分（c2）。
	rep2 := s.CheckStudent("s2")
	if rep2.TotalCredits != 3 || len(rep2.Unmet) != 0 {
		t.Fatalf("s2 应凭通过修读 e2 获得 3 学分，选课不增加也不抵消学分，得到 %+v", rep2)
	}
	st := rep2.Requirements[0]
	if st.Source != "enrollment" || st.PassedEnrollmentID != "e2" ||
		len(st.PassedEnrollmentIDs) != 1 {
		t.Fatalf("s2 的来源应仅为本人通过修读 e2，得到 %+v", st)
	}
	if e := s.Enrollment("s2", "e1"); e == nil || e.Result != Enrolled {
		t.Fatalf("s2 的选课 e1 应保留，得到 %+v", e)
	}

	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != string(raw) {
		t.Fatalf("读取合法记录不得改写原文件：%v %q", readErr, got)
	}
}

// TestLoadSharedEnrollmentIDWithDistinctSeqsKeptApart 不同学生共用修读编号
// （以及要求编号、学期）但结果提交序号不同是合法记录：两份修读各自保留
// 所属学生、结果与序号，核对互不串扰；已提交序号允许有间隔，不要求连续。
func TestLoadSharedEnrollmentIDWithDistinctSeqsKeptApart(t *testing.T) {
	d := seqBaseRecord(t, true)
	d.Enrollments = []*Enrollment{
		// 两人共用编号 e1、同学期、同编号要求，但序号不同（1 与 4，留间隔）。
		seqEnr("s1", "e1", "2024春", Passed, 1),
		seqEnr("s2", "e1", "2024春", Failed, 4),
		// s1 的第二条通过与 s2 的第二条通过也共用编号 e2，序号继续不同。
		seqEnr("s1", "e2", "2024秋", Passed, 9),
		seqEnr("s2", "e2", "2024秋", Passed, 12),
	}
	d.NextResultSeq = 12
	path, raw := writeRecord(t, d)

	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("跨学生共用编号但序号不同应正常读取，existed=%v err=%v", existed, err)
	}

	// s1：e1 通过、e2 也通过，同一要求多次通过只计 4 学分，来源是序号 1 的 e1。
	rep1 := s.CheckStudent("s1")
	if rep1.TotalCredits != 4 || len(rep1.Unmet) != 0 {
		t.Fatalf("s1 应得 4 学分且要求满足，得到学分=%d 未满足=%v", rep1.TotalCredits, rep1.Unmet)
	}
	st1 := rep1.Requirements[0]
	if st1.Source != "enrollment" || st1.PassedEnrollmentID != "e1" {
		t.Fatalf("s1 来源应是本人序号最小的通过修读 e1，得到 %+v", st1)
	}
	if len(st1.PassedEnrollmentIDs) != 2 ||
		st1.PassedEnrollmentIDs[0] != "e1" || st1.PassedEnrollmentIDs[1] != "e2" {
		t.Fatalf("s1 的通过历史应只含本人的 [e1 e2]，得到 %v", st1.PassedEnrollmentIDs)
	}

	// s2：e1 未通过、e2 通过，只计自己课程 c2 的 3 学分，来源是 e2，
	// 未通过的 e1 保留但不成为来源。
	rep2 := s.CheckStudent("s2")
	if rep2.TotalCredits != 3 || len(rep2.Unmet) != 0 {
		t.Fatalf("s2 应凭 e2 通过获得 3 学分，得到学分=%d 未满足=%v", rep2.TotalCredits, rep2.Unmet)
	}
	st2 := rep2.Requirements[0]
	if st2.Source != "enrollment" || st2.PassedEnrollmentID != "e2" ||
		len(st2.PassedEnrollmentIDs) != 1 || st2.PassedEnrollmentIDs[0] != "e2" {
		t.Fatalf("s2 的来源应仅为本人的通过修读 e2，得到 %+v", st2)
	}

	// 同编号修读是两条独立记录，结果与序号各归各。
	e1s1 := s.Enrollment("s1", "e1")
	e1s2 := s.Enrollment("s2", "e1")
	if e1s1 == nil || e1s2 == nil || e1s1 == e1s2 {
		t.Fatalf("两名学生的同号修读应各自独立，得到 %p %p", e1s1, e1s2)
	}
	if e1s1.Result != Passed || e1s1.ResultSeq != 1 {
		t.Fatalf("s1 的 e1 应是序号 1 的通过，得到 %+v", e1s1)
	}
	if e1s2.Result != Failed || e1s2.ResultSeq != 4 {
		t.Fatalf("s2 的 e1 应是序号 4 的未通过，得到 %+v", e1s2)
	}

	// 恢复后的序号计数器继续正确分配：下一次提交拿到 13，且不与历史冲突。
	mustEnroll(t, s, "s1", "r1", "2026春", "e9")
	e9, changed, err := s.SubmitResult("s1", "e9", Passed)
	if err != nil || !changed || e9.ResultSeq != 13 {
		t.Fatalf("计数器恢复后下一次提交应分配序号 13，得到 %+v changed=%v err=%v",
			e9, changed, err)
	}

	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != string(raw) {
		t.Fatalf("读取与只读核对不得改写原文件：%v", readErr)
	}
}
