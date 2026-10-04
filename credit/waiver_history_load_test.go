package credit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件为“读取已有记录时，同一学生的同一要求只能有一份有效免修”这项
// 业务规则提供回归保障。冲突无法经正常申请流程产生（第二份会在提交时被
// 拒绝），因此全部用例直接构造记录文件，聚焦 Load 读取历史这一路径：
//   - 文件本身完整、可解析，学生、课程、要求确实存在，两份免修依据非空，
//     只要同一学生同一要求下有两份编号不同的 approved 免修就整份拒绝；
//   - 一份 approved 与 rejected/revoked 历史共存是合法记录：核对只认那份
//     有效免修、学分只计一次，历史与原有原因逐条保留，失效申请不重新生效；
//   - 唯一性按“学生 + 要求”界定：不同学生共享要求编号、免修编号互不影响。

// writeRecord 将 d 序列化到临时记录文件，返回路径与写入的原始字节，
// 供调用方在拒绝读取后逐字节比对。
func writeRecord(t *testing.T, d *fileData) (path string, raw []byte) {
	t.Helper()
	buf, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		t.Fatalf("序列化测试记录失败：%v", err)
	}
	buf = append(buf, '\n')
	path = filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("写入测试记录失败：%v", err)
	}
	return path, buf
}

// waiverConflictBase 构造一份结构完整、引用齐全的基础记录：
// 学生 s1 的要求 r1 指向 4 学分课程 c1。可选附一条已通过修读 e1。
func waiverConflictBase(withPass bool) *fileData {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
		},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
	}
	if withPass {
		d.Enrollments = []*Enrollment{{
			ID: "e1", StudentID: "s1", ReqID: "r1", Term: "2024春",
			Result: Passed, ResultSeq: 1,
		}}
		d.NextResultSeq = 1
	}
	return d
}

// assertConflictRejected 是冲突用例的共同断言：Load 必须失败、文件标记为
// 已存在，错误点名问题文件、所属学生、目标要求与冲突的两个免修编号，
// 且原文件字节完整保留。
func assertConflictRejected(t *testing.T, path string, raw []byte, student, req, w1, w2 string) {
	t.Helper()
	s, existed, err := Load(path)
	if err == nil {
		t.Fatalf("同一学生同一要求存在两份有效免修时必须整份拒绝，却得到 store=%v", s)
	}
	if !existed {
		t.Fatalf("冲突记录应标记为文件已存在，existed=%v err=%v", existed, err)
	}
	msg := err.Error()
	for _, want := range []string{path, "内容损坏", student, req, w1, w2, "有效免修"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误信息应包含 %q（文件/学生/要求/冲突免修编号），得到：%v", want, err)
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

// TestLoadRejectsDuplicateApprovedWaiversInHistory 文件完整可解析、引用齐全、
// 两份免修都有非空依据，但同一学生同一要求下有两份编号不同的有效免修：
// 无论它们在历史中怎样排列、是否夹杂失效申请、是否另有通过修读、是否还有
// 另一名学生的合法同号记录，都必须整份拒绝并保留原文件。
func TestLoadRejectsDuplicateApprovedWaiversInHistory(t *testing.T) {
	approved := func(id, basis string) *Waiver {
		return &Waiver{ID: id, StudentID: "s1", ReqID: "r1", Basis: basis, Status: WaiverApproved}
	}

	t.Run("两份有效免修_先w1后w2", func(t *testing.T) {
		d := waiverConflictBase(false)
		d.Waivers = []*Waiver{approved("w1", "学科竞赛获奖"), approved("w2", "外校同层次课程")}
		path, raw := writeRecord(t, d)
		assertConflictRejected(t, path, raw, "s1", "r1", "w1", "w2")
		// 先读到的编号应排在冲突说明前面，证明两份都被点名而不是只报后者。
		if _, _, err := Load(path); err == nil ||
			!strings.Contains(err.Error(), "有效免修 w1 与 w2") {
			t.Fatalf("冲突说明应按读取顺序点名 w1 与 w2，得到：%v", err)
		}
	})

	t.Run("两份有效免修_先w2后w1", func(t *testing.T) {
		d := waiverConflictBase(false)
		d.Waivers = []*Waiver{approved("w2", "外校同层次课程"), approved("w1", "学科竞赛获奖")}
		path, raw := writeRecord(t, d)
		assertConflictRejected(t, path, raw, "s1", "r1", "w1", "w2")
		if _, _, err := Load(path); err == nil ||
			!strings.Contains(err.Error(), "有效免修 w2 与 w1") {
			t.Fatalf("交换排列后冲突说明应点名 w2 与 w1，不能只报固定的一份，得到：%v", err)
		}
	})

	t.Run("失效申请夹在中间不改变冲突", func(t *testing.T) {
		d := waiverConflictBase(false)
		d.Waivers = []*Waiver{
			approved("w1", "学科竞赛获奖"),
			{ID: "wx", StudentID: "s1", ReqID: "r1", Basis: "旧依据",
				Status: WaiverRejected, Reason: "该要求已有有效免修 w1"},
			approved("w2", "外校同层次课程"),
		}
		path, raw := writeRecord(t, d)
		assertConflictRejected(t, path, raw, "s1", "r1", "w1", "w2")
	})

	t.Run("另有通过修读不能掩盖免修冲突", func(t *testing.T) {
		// 即使该要求已通过修读满足，两份有效免修并存仍然是记录损坏：
		// 不能拿着通过记录继续核对学分。
		d := waiverConflictBase(true)
		d.Waivers = []*Waiver{approved("w1", "学科竞赛获奖"), approved("w2", "外校同层次课程")}
		path, raw := writeRecord(t, d)
		assertConflictRejected(t, path, raw, "s1", "r1", "w1", "w2")
	})

	t.Run("无关学生的合法同号记录不背锅", func(t *testing.T) {
		// s2 各自只有一份有效免修，完全合法；s1 名下的冲突仍要整份拒绝，
		// 且错误应归属 s1，不能误报成 s2 的同一要求被重复取代。
		d := waiverConflictBase(false)
		d.Courses = append(d.Courses, &Course{ID: "c2", Name: "线性代数", Credit: 3, Open: true})
		d.Students = append(d.Students, &Student{ID: "s2"})
		d.Requirements = append(d.Requirements, &Requirement{ID: "r1", StudentID: "s2", CourseID: "c2"})
		d.Waivers = []*Waiver{
			approved("w1", "学科竞赛获奖"),
			approved("w2", "外校同层次课程"),
			{ID: "w1", StudentID: "s2", ReqID: "r1", Basis: "s2 的依据", Status: WaiverApproved},
		}
		path, raw := writeRecord(t, d)
		s, existed, err := Load(path)
		if err == nil {
			t.Fatalf("s1 名下冲突时必须整份拒绝，不能只加载 s2 的合法部分，得到 store=%v", s)
		}
		if !existed {
			t.Fatalf("冲突记录应标记为已存在，existed=%v", existed)
		}
		msg := err.Error()
		if !strings.Contains(msg, "s1") || !strings.Contains(msg, "w1") || !strings.Contains(msg, "w2") {
			t.Fatalf("冲突应归属 s1 并点名 w1/w2，得到：%v", err)
		}
		if strings.Contains(msg, "学生 s2") {
			t.Fatalf("s2 只有一份有效免修，不应被点名为冲突方，得到：%v", err)
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

// mixedHistory 构造同一要求 r1 下三种状态各一份的免修历史，
// 排列由调用方给定。
func mixedHistory(order []string) []*Waiver {
	byID := map[string]*Waiver{
		"wa": {ID: "wa", StudentID: "s1", ReqID: "r1",
			Basis: "外校同层次课程", Status: WaiverApproved},
		"wr": {ID: "wr", StudentID: "s1", ReqID: "r1",
			Basis: "再次申请的新依据", Status: WaiverRejected, Reason: "该要求已有有效免修 wa"},
		"wv": {ID: "wv", StudentID: "s1", ReqID: "r1",
			Basis: "原竞赛材料", Status: WaiverRevoked, Reason: "材料无法核实"},
	}
	var ws []*Waiver
	for _, id := range order {
		ws = append(ws, byID[id])
	}
	return ws
}

// assertMixedHistoryLoaded 核对加载后的记录：唯一有效免修 wa 说明来源、
// 课程学分只计一次，被拒绝与已撤销历史逐条保留原编号、依据、状态与原因。
func assertMHistoryLoaded(t *testing.T, s *Store, order []string, withPass bool) {
	t.Helper()
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 4 {
		t.Fatalf("只应以一份有效免修计一次 4 学分，得到 %d", rep.TotalCredits)
	}
	if len(rep.Unmet) != 0 {
		t.Fatalf("r1 应由有效免修满足，未满足=%v", rep.Unmet)
	}
	if len(rep.Requirements) != 1 {
		t.Fatalf("应只有一项要求，得到 %d", len(rep.Requirements))
	}
	st := rep.Requirements[0]
	if !st.Satisfied || st.Source != "waiver" || st.WaiverID != "wa" {
		t.Fatalf("来源应是唯一有效免修 wa，失效申请不能参与满足，得到 %+v", st)
	}
	if withPass {
		if len(st.PassedEnrollmentIDs) != 1 || st.PassedEnrollmentIDs[0] != "e1" {
			t.Fatalf("通过修读历史应保留但不重复计学分，得到 %v", st.PassedEnrollmentIDs)
		}
	} else if len(st.PassedEnrollmentIDs) != 0 {
		t.Fatalf("没有修读时通过历史应为空，得到 %v", st.PassedEnrollmentIDs)
	}

	// 被拒绝与已撤销申请各按原记录列出，原因保留。
	if len(rep.RejectedWaivers) != 1 {
		t.Fatalf("应保留 1 条被拒绝申请，得到 %d", len(rep.RejectedWaivers))
	}
	rj := rep.RejectedWaivers[0]
	if rj.Waiver.ID != "wr" || rj.Waiver.Basis != "再次申请的新依据" ||
		rj.Waiver.Status != WaiverRejected || rj.Reason != "该要求已有有效免修 wa" {
		t.Fatalf("被拒绝记录应保留原编号、依据、状态与原因，得到 %+v", rj)
	}
	if len(rep.RevokedWaivers) != 1 || rep.RevokedWaivers[0] != "wv" {
		t.Fatalf("应列出 1 条已撤销免修 wv，得到 %v", rep.RevokedWaivers)
	}

	// 免修历史完整保留三条且顺序与文件一致，不能为去重丢掉任何一条。
	ws := s.Waivers("s1")
	if len(ws) != 3 {
		t.Fatalf("免修历史应完整保留 3 条，得到 %d", len(ws))
	}
	for i, wantID := range order {
		if ws[i].ID != wantID {
			t.Fatalf("免修历史顺序应与记录一致，第 %d 条想要 %s 得到 %s（全部=%v）",
				i, wantID, ws[i].ID, ws)
		}
	}
	if w := s.Waiver("s1", "wv"); w == nil || w.Status != WaiverRevoked ||
		w.Basis != "原竞赛材料" || w.Reason != "材料无法核实" {
		t.Fatalf("已撤销免修应保留原依据与撤销原因，得到 %+v", w)
	}
	if w := s.Waiver("s1", "wr"); w == nil || w.Status != WaiverRejected ||
		w.Reason != "该要求已有有效免修 wa" {
		t.Fatalf("已拒绝免修应保留原拒绝原因，得到 %+v", w)
	}
	if w := s.Waiver("s1", "wa"); w == nil || w.Status != WaiverApproved {
		t.Fatalf("wa 应保持有效，得到 %+v", w)
	}
}

// TestLoadMixedWaiverHistoryUsesSingleValidSource 同一要求下一份有效免修与
// 已拒绝、已撤销申请共存是合法记录：历史排在有效申请之前或之后结论相同；
// 另有通过修读时仍只计一次学分、以免修说明来源；重新保存再加载历史不丢失。
func TestLoadMixedWaiverHistoryUsesSingleValidSource(t *testing.T) {
	orders := map[string][]string{
		"失效历史在有效之后": {"wa", "wr", "wv"},
		"失效历史在有效之前": {"wr", "wv", "wa"},
		"有效申请夹在中间":  {"wr", "wa", "wv"},
	}
	for name, order := range orders {
		t.Run(name, func(t *testing.T) {
			d := waiverConflictBase(false)
			d.Waivers = mixedHistory(order)
			path, _ := writeRecord(t, d)
			s, existed, err := Load(path)
			if err != nil || !existed {
				t.Fatalf("一份有效与失效历史共存应正常打开，existed=%v err=%v", existed, err)
			}
			assertMHistoryLoaded(t, s, order, false)
		})
	}

	// 有效免修与通过修读并存：历史排列任意，学分仍只计一次、来源仍是免修。
	d := waiverConflictBase(true)
	d.Waivers = mixedHistory([]string{"wr", "wv", "wa"})
	path, _ := writeRecord(t, d)
	s, _, err := Load(path)
	if err != nil {
		t.Fatalf("含通过修读与混合历史的合法记录应可加载：%v", err)
	}
	assertMHistoryLoaded(t, s, []string{"wr", "wv", "wa"}, true)

	// 重新保存后再次加载：混合历史、来源与学分完全保持，不会在下一次读取时
	// 被误判为冲突。
	path2 := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path2); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, existed, err := Load(path2)
	if err != nil || !existed {
		t.Fatalf("混合历史重新保存后应可再次加载，existed=%v err=%v", existed, err)
	}
	assertMHistoryLoaded(t, reloaded, []string{"wr", "wv", "wa"}, true)
}

// TestLoadInvalidWaiversAloneDoNotSatisfyRequirement 只有已拒绝与已撤销申请、
// 没有有效免修时记录合法，但要求必须未满足、学分为零：失效申请不能重新参与
// 满足要求；历史与原因仍完整可查。
func TestLoadInvalidWaiversAloneDoNotSatisfyRequirement(t *testing.T) {
	d := waiverConflictBase(false)
	d.Waivers = []*Waiver{
		// 被拒绝申请允许依据为空（读取只要求有效免修依据非空）。
		{ID: "wr", StudentID: "s1", ReqID: "r1", Basis: "",
			Status: WaiverRejected, Reason: "免修依据为空"},
		{ID: "wv", StudentID: "s1", ReqID: "r1", Basis: "原竞赛材料",
			Status: WaiverRevoked, Reason: "材料无法核实"},
	}
	path, _ := writeRecord(t, d)
	s, _, err := Load(path)
	if err != nil {
		t.Fatalf("仅含失效申请的记录应正常打开：%v", err)
	}
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 0 {
		t.Fatalf("失效免修不应产生学分，得到 %d", rep.TotalCredits)
	}
	if len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("r1 应未满足，得到 %v", rep.Unmet)
	}
	st := rep.Requirements[0]
	if st.Satisfied || st.Source != "" || st.WaiverID != "" {
		t.Fatalf("失效申请不能成为来源，得到 %+v", st)
	}
	if len(rep.RejectedWaivers) != 1 || rep.RejectedWaivers[0].Waiver.ID != "wr" ||
		rep.RejectedWaivers[0].Reason != "免修依据为空" {
		t.Fatalf("应保留被拒绝记录与原因，得到 %+v", rep.RejectedWaivers)
	}
	if len(rep.RevokedWaivers) != 1 || rep.RevokedWaivers[0] != "wv" {
		t.Fatalf("应保留已撤销记录 wv，得到 %v", rep.RevokedWaivers)
	}
	if ws := s.Waivers("s1"); len(ws) != 2 || ws[0].ID != "wr" || ws[1].ID != "wv" {
		t.Fatalf("两条失效历史都应保留且顺序不变，得到 %+v", ws)
	}
}

// TestLoadSameReqAndWaiverIDAcrossStudentsLoadsSeparately 唯一性只按
// “同一学生名下的同一要求”界定：两名学生共用要求编号 r1、共用免修编号 w1，
// 各自只有一份有效申请时记录合法，核对分别得到本人课程的学分与本人的免修
// 来源，不能误报为同一要求被重复取代。同一学生的不同要求也可各有一份免修。
func TestLoadSameReqAndWaiverIDAcrossStudentsLoadsSeparately(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s1"}, {ID: "s2"}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: "c1"},
			{ID: "r2", StudentID: "s1", CourseID: "c2"},
			{ID: "r1", StudentID: "s2", CourseID: "c2"},
		},
		Waivers: []*Waiver{
			{ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "s1-r1 依据", Status: WaiverApproved},
			{ID: "w2", StudentID: "s1", ReqID: "r2", Basis: "s1-r2 依据", Status: WaiverApproved},
			{ID: "w1", StudentID: "s2", ReqID: "r1", Basis: "s2-r1 依据", Status: WaiverApproved},
		},
	}
	path, raw := writeRecord(t, d)
	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("跨学生共享编号且各自唯一应正常读取，existed=%v err=%v", existed, err)
	}

	// s1：两项要求共 4+3=7 学分，来源分别是本人的 w1、w2，课程各不相同。
	rep1 := s.CheckStudent("s1")
	if rep1.TotalCredits != 7 || len(rep1.Unmet) != 0 {
		t.Fatalf("s1 应得 7 学分且全部满足，得到学分=%d 未满足=%v", rep1.TotalCredits, rep1.Unmet)
	}
	if st := rep1.Requirements[0]; st.Req.ID != "r1" || st.Source != "waiver" ||
		st.WaiverID != "w1" || st.Course.ID != "c1" || st.Course.Credit != 4 {
		t.Fatalf("s1 的 r1 应由本人 w1 对应 4 学分课程满足，得到 %+v", st)
	}
	if st := rep1.Requirements[1]; st.Req.ID != "r2" || st.Source != "waiver" ||
		st.WaiverID != "w2" || st.Course.ID != "c2" || st.Course.Credit != 3 {
		t.Fatalf("s1 的 r2 应由本人 w2 对应 3 学分课程满足，得到 %+v", st)
	}

	// s2：同号 r1/w1，但是本人的 3 学分课程与本人依据。
	rep2 := s.CheckStudent("s2")
	if rep2.TotalCredits != 3 || len(rep2.Unmet) != 0 {
		t.Fatalf("s2 应得本人课程的 3 学分，得到学分=%d 未满足=%v", rep2.TotalCredits, rep2.Unmet)
	}
	if st := rep2.Requirements[0]; st.Source != "waiver" || st.WaiverID != "w1" ||
		st.Course.ID != "c2" || st.Course.Credit != 3 {
		t.Fatalf("s2 的 r1 应由本人 w1 对应本人 3 学分课程满足，得到 %+v", st)
	}

	// 同号免修是两条独立记录，归属与依据各归各。
	w1s1 := s.Waiver("s1", "w1")
	w1s2 := s.Waiver("s2", "w1")
	if w1s1 == nil || w1s2 == nil || w1s1 == w1s2 {
		t.Fatalf("两名学生的同号免修应各自独立，得到 %p %p", w1s1, w1s2)
	}
	if w1s1.Basis != "s1-r1 依据" || w1s2.Basis != "s2-r1 依据" ||
		w1s1.StudentID != "s1" || w1s2.StudentID != "s2" {
		t.Fatalf("同号免修的依据应各归本人，得到 %+v %+v", w1s1, w1s2)
	}

	// 合法记录上的只读核对不得改动文件。
	_ = s.CheckStudent("s1")
	_ = s.CheckStudent("s2")
	_ = s.Waivers("s1")
	_ = s.Waivers("s2")
	if s.Dirty() {
		t.Fatal("只读查询不应把记录标记为已变更")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(raw) {
		t.Fatal("只读查询不得改写合法记录文件")
	}
}
