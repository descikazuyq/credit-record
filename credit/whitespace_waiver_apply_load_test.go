package credit

import (
	"path/filepath"
	"strings"
	"testing"
)

// 本文件为“申请免修时按用户给出的完整编号确定学生、要求与原申请”这项
// 行为提供回归保障。记录文件可以合法保存带前后空白的学生编号、要求编号与
// 免修编号（登记入口会修剪编号，但既有文件中的编号原文不得改写，Load 也
// 按原样接受），因此基础数据直接构造记录文件，聚焦 waiver 的选对象逻辑：
//   - “s1”与“ s1 ”是两名不同学生，同一学生名下“r1”与“ r1 ”是两项
//     不同要求，“w1”与“ w1 ”是两份不同申请；普通空格、制表符、全角
//     空格（U+3000）都是编号内容；
//   - 给带空白的完整编号提交申请只命中编号完全一致的学生、要求与原申请，
//     另一名学生或另一份编号的学分、要求与历史保持原样；
//   - 完整学生编号找不到时直接报学生不存在（不留任何历史）；学生存在但
//     名下没有该完整要求编号时按目标要求不存在拒绝，拒绝记录按原规则保
//     存在申请人名下，保留完整的要求编号、免修编号与原因——即使去掉空白
//     能碰上另一名学生或另一项要求，也绝不借用那份记录；
//   - 只有空字符串编号按“不能为空”拒绝；依据的空白口径与原文保留不变；
//   - 准确命中后沿用现有规则：同内容返回原申请，内容冲突拒绝修改，已拒绝
//     或已撤销的申请不会因重试重新生效，新编号申请已有有效免修的要求只
//     留下一条被拒绝的新申请。

// waiverWhitespaceBase 构造两名学生 "s1" 与 " s1 "：各自编号为 r1 的要求
// 分别指向 4 学分课程 c1 与 3 学分课程 c2，两人都没有修读或免修。
func waiverWhitespaceBase() *fileData {
	return &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s1"}, {ID: " s1 "}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: "c1"},
			{ID: "r1", StudentID: " s1 ", CourseID: "c2"},
		},
	}
}

// mustLoadWaiverWhitespace 加载含前后空白编号的合法记录：不得判为损坏。
func mustLoadWaiverWhitespace(t *testing.T, d *fileData) *Store {
	t.Helper()
	path, _ := writeRecord(t, d)
	s, _, err := Load(path)
	if err != nil {
		t.Fatalf("含前后空白编号的记录是合法记录，不应判为损坏：%v", err)
	}
	return s
}

// TestApplyWaiverWhitespaceStudentIDHitsExactOwner 两名学生 "s1" 与 " s1 "
// 各有一项同号要求 r1，分别指向 4 学分和 3 学分课程且都没有修读或免修：
// 给 " s1 " 提交申请只能命中本人的要求，本人凭该有效免修获得 3 学分，
// 申请记录归属 " s1 "；"s1" 的学分、要求与免修历史保持原样。
func TestApplyWaiverWhitespaceStudentIDHitsExactOwner(t *testing.T) {
	s := mustLoadWaiverWhitespace(t, waiverWhitespaceBase())

	w, a, err := s.ApplyWaiver(" s1 ", "r1", "w9", "学科竞赛获奖证明")
	if err != nil || a != ActionCreated || w.Status != WaiverApproved {
		t.Fatalf("给 \" s1 \" 的 r1 申请免修应正常生效，得到 %+v action=%v err=%v", w, a, err)
	}
	if w.StudentID != " s1 " || w.ReqID != "r1" || w.ID != "w9" ||
		w.Basis != "学科竞赛获奖证明" {
		t.Fatalf("申请应按完整编号登记在 \" s1 \" 名下，得到 %+v", w)
	}

	// " s1 " 本人：要求满足、3 学分、来源为本人的有效免修 w9。
	rep := s.CheckStudent(" s1 ")
	if !rep.Found || rep.TotalCredits != 3 || len(rep.Unmet) != 0 {
		t.Fatalf("\" s1 \" 应凭本人要求获得 3 学分，得到 %+v", rep)
	}
	if len(rep.Requirements) != 1 {
		t.Fatalf("\" s1 \" 应只有一项要求，得到 %d 项", len(rep.Requirements))
	}
	st := rep.Requirements[0]
	if st.Req.ID != "r1" || st.Course == nil || st.Course.ID != "c2" ||
		st.Source != "waiver" || st.WaiverID != "w9" || !st.Satisfied {
		t.Fatalf("\" s1 \" 的来源应是本人 r1（课程 c2，3 学分）的有效免修 w9，得到 %+v", st)
	}
	if len(rep.RejectedWaivers) != 0 {
		t.Fatalf("\" s1 \" 不应有被拒绝的申请，得到 %+v", rep.RejectedWaivers)
	}

	// "s1" 保持原样：0 学分、r1 未满足、没有任何免修历史。
	if got := s.Waiver("s1", "w9"); got != nil {
		t.Fatalf("申请不应记到 \"s1\" 名下，得到 %+v", got)
	}
	if ws := s.Waivers("s1"); len(ws) != 0 {
		t.Fatalf("\"s1\" 的免修历史应为空，得到 %+v", ws)
	}
	rep = s.CheckStudent("s1")
	if !rep.Found || rep.TotalCredits != 0 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("\"s1\" 应仍为 0 学分且 r1 未满足，得到 %+v", rep)
	}
	if rep.Requirements[0].Satisfied || rep.Requirements[0].Source != "" {
		t.Fatalf("\"s1\" 的要求不应有任何来源，得到 %+v", rep.Requirements[0])
	}

	// 保存后重新打开：申请仍归属 " s1 "，结论不变。
	path := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("含带空白编号申请的文件应正常打开，existed=%v err=%v", existed, err)
	}
	if w := reloaded.Waiver(" s1 ", "w9"); w == nil || w.Status != WaiverApproved ||
		w.ReqID != "r1" {
		t.Fatalf("重新打开后申请仍应属于 \" s1 \"，得到 %+v", w)
	}
	if rep := reloaded.CheckStudent(" s1 "); rep.TotalCredits != 3 ||
		rep.Requirements[0].WaiverID != "w9" {
		t.Fatalf("重新打开后 \" s1 \" 仍应凭 w9 获得 3 学分，得到 %+v", rep)
	}
	if ws := reloaded.Waivers("s1"); len(ws) != 0 {
		t.Fatalf("重新打开后 \"s1\" 名下仍不应出现该申请，得到 %+v", ws)
	}
}

// TestApplyWaiverWhitespaceReqIDIndependent 同一学生名下的 "r1" 与 " r1 "
// 是两项不同要求：给 " r1 " 提交申请只满足该完整编号对应的要求，另一项
// 要求仍未满足，既不误命中也不合并。
func TestApplyWaiverWhitespaceReqIDIndependent(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s1"}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: "c1"},
			{ID: " r1 ", StudentID: "s1", CourseID: "c2"},
		},
	}
	s := mustLoadWaiverWhitespace(t, d)

	w, a, err := s.ApplyWaiver("s1", " r1 ", "w1", "外校同层次课程证明")
	if err != nil || a != ActionCreated || w.Status != WaiverApproved {
		t.Fatalf("给 \" r1 \" 申请免修应正常生效，得到 %+v action=%v err=%v", w, a, err)
	}
	if w.ReqID != " r1 " || w.ID != "w1" {
		t.Fatalf("申请应保留完整要求编号 \" r1 \"，得到 %+v", w)
	}

	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 3 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("只有 \" r1 \" 满足并计 3 学分，r1 应未满足，得到 学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	var satisfied, unmet *RequirementStatus
	for i := range rep.Requirements {
		if rep.Requirements[i].Satisfied {
			satisfied = &rep.Requirements[i]
		} else {
			unmet = &rep.Requirements[i]
		}
	}
	if satisfied == nil || satisfied.Req.ID != " r1 " ||
		satisfied.Source != "waiver" || satisfied.WaiverID != "w1" ||
		satisfied.Course == nil || satisfied.Course.ID != "c2" {
		t.Fatalf("满足的应是 \" r1 \"、来源为 w1，得到 %+v", satisfied)
	}
	if unmet == nil || unmet.Req.ID != "r1" || unmet.Source != "" {
		t.Fatalf("r1 应保持未满足且无来源，得到 %+v", unmet)
	}
	// 完整编号不同的要求下没有申请。
	if w := s.validWaiver("s1", "r1"); w != nil {
		t.Fatalf("r1 下不应出现免修，得到 %+v", w)
	}
}

// TestApplyWaiverWhitespaceWaiverIDIndependent 同一学生名下 "w1" 与 " w1 "
// 是两份不同申请：r1 已有有效免修 w1 时，用 " w1 " 提交的申请既不能返回
// w1（幂等命中）也不能按内容冲突拒绝修改 w1，而是独立新建一条被拒绝的
// 申请；两份申请各自保留，重试与冲突规则只作用于完整编号相同的那一份。
func TestApplyWaiverWhitespaceWaiverIDIndependent(t *testing.T) {
	d := &fileData{
		Version:      recordVersion,
		Courses:      []*Course{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
		Waivers: []*Waiver{
			{ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "原获奖依据", Status: WaiverApproved},
		},
	}
	s := mustLoadWaiverWhitespace(t, d)
	w1 := s.Waiver("s1", "w1")

	// 新编号 " w1 " 申请同一项已有有效免修的要求：新建一条被拒绝记录，
	// 不能产生第二份有效免修。
	w2, a, err := s.ApplyWaiver("s1", "r1", " w1 ", "再次申请的新依据")
	if err != nil || a != ActionCreated || w2.Status != WaiverRejected {
		t.Fatalf("新编号 \" w1 \" 取代已有免修应新建被拒绝记录，得到 %+v action=%v err=%v",
			w2, a, err)
	}
	if w2.ID != " w1 " || w2.ReqID != "r1" || w2.Basis != "再次申请的新依据" ||
		!strings.Contains(w2.Reason, "w1") {
		t.Fatalf("拒绝记录应保留完整编号 \" w1 \"、原要求、依据并点名 w1，得到 %+v", w2)
	}
	if w1 == w2 || s.Waiver("s1", " w1 ") != w2 {
		t.Fatal("\"w1\" 与 \" w1 \" 应是各自独立的两份申请")
	}
	if ws := s.Waivers("s1"); len(ws) != 2 || ws[0] != w1 || ws[1] != w2 {
		t.Fatalf("免修历史应同时保留两份申请，得到 %+v", ws)
	}

	// 学分来源仍是原有效免修 w1，总学分只计一次。
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
		t.Fatalf("r1 应仍由 w1 满足、计 4 学分，得到 学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	if rep.Requirements[0].WaiverID != "w1" || len(rep.RejectedWaivers) != 1 ||
		rep.RejectedWaivers[0].Waiver != w2 {
		t.Fatalf("来源应仍是 w1，被拒绝列表应只含 \" w1 \"，得到 %+v", rep)
	}

	// 原样重试 " w1 "：返回它自己的原拒绝结果，不返回 w1，也不重新生效。
	if got, a, err := s.ApplyWaiver("s1", "r1", " w1 ", "再次申请的新依据"); err != nil ||
		a != ActionExisted || got != w2 || got.Status != WaiverRejected {
		t.Fatalf("重试 \" w1 \" 应返回其本人的原拒绝记录，得到 %+v action=%v err=%v",
			got, a, err)
	}
	// " w1 " 换内容：按内容冲突拒绝，命中的是 " w1 " 自己，不能报 w1 的内容。
	if _, _, err := s.ApplyWaiver("s1", "r1", " w1 ", "改换依据"); err == nil {
		t.Fatal("\" w1 \" 改换依据应被拒绝")
	} else if !strings.Contains(err.Error(), " w1 ") {
		t.Fatalf("冲突拒绝应点名完整免修编号 \" w1 \"，得到 %v", err)
	}
	// 用原编号 w1 换内容：冲突必须命中 w1 本身（消息含单个空格的原编号与
	// w1 的原依据），不能撞上编号为 " w1 " 的那条拒绝记录。
	if _, _, err := s.ApplyWaiver("s1", "r1", "w1", "改换依据"); err == nil {
		t.Fatal("w1 改换依据应被拒绝")
	} else {
		msg := err.Error()
		if !strings.Contains(msg, "免修编号 w1 已存在") ||
			!strings.Contains(msg, "原获奖依据") {
			t.Fatalf("冲突拒绝应点名原申请 w1 及其原依据，得到 %v", err)
		}
		if strings.Contains(msg, "免修编号  w1  已存在") {
			t.Fatalf("冲突不应命中带空白的 \" w1 \"，得到 %v", err)
		}
	}
	// 任何冲突都不改动两份申请。
	if got := s.Waiver("s1", "w1"); got != w1 || got.Status != WaiverApproved ||
		got.Basis != "原获奖依据" {
		t.Fatalf("w1 应保持有效且依据不变，得到 %+v", got)
	}
	if got := s.Waiver("s1", " w1 "); got != w2 || got.Status != WaiverRejected {
		t.Fatalf("\" w1 \" 应保持被拒绝，得到 %+v", got)
	}
	if n := len(s.Waivers("s1")); n != 2 {
		t.Fatalf("冲突与重试都不应新增申请，历史应仍为 2 条，得到 %d", n)
	}
}

// TestApplyWaiverWhitespaceUnknownStudentNoHistory 完整学生编号不存在时
// 直接报学生不存在：即使去掉空白能碰上 "s1"，也绝不借用其名义提交，不
// 给任何学生新增免修历史，也不产生变更。
func TestApplyWaiverWhitespaceUnknownStudentNoHistory(t *testing.T) {
	d := &fileData{
		Version:      recordVersion,
		Courses:      []*Course{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
	}
	for _, student := range []string{" s1", "s1 ", " s1 ", "\ts1\t", "　s1　"} {
		s := mustLoadWaiverWhitespace(t, d)
		w, _, err := s.ApplyWaiver(student, "r1", "w1", "依据文字")
		if err == nil {
			t.Fatalf("学生 %q 不存在应直接拒绝，却得到申请 %+v", student, w)
		}
		msg := err.Error()
		if !strings.Contains(msg, "学生") || !strings.Contains(msg, "不存在") {
			t.Fatalf("学生 %q 找不到时应明确报告学生不存在，得到 %v", student, err)
		}
		if w != nil {
			t.Fatalf("学生 %q 不存在时不应返回任何申请记录，得到 %+v", student, w)
		}
		if s.Dirty() {
			t.Fatalf("学生 %q 被拒绝不应产生任何变更", student)
		}
		if ws := s.Waivers("s1"); len(ws) != 0 {
			t.Fatalf("学生 %q 的拒绝不应记到 \"s1\" 名下，得到 %+v", student, ws)
		}
		if ws := s.Waivers(student); len(ws) != 0 {
			t.Fatalf("不应为不存在的学生 %q 建立免修历史，得到 %+v", student, ws)
		}
		if rep := s.CheckStudent("s1"); rep.TotalCredits != 0 ||
			len(rep.RejectedWaivers) != 0 || len(rep.Unmet) != 1 {
			t.Fatalf("学生 %q 被拒绝后 \"s1\" 的核对结果应不变，得到 %+v", student, rep)
		}
	}
}

// TestApplyWaiverWhitespaceMissingReqSavedUnderApplicant 学生存在但完整
// 要求编号不存在时，即使去掉空白能找到要求，也按目标要求不存在拒绝该
// 申请：拒绝记录保存在申请人名下，保留原要求编号、免修编号与原因，
// 不获得学分；被去空白碰上的那项要求及其学生不受影响。
func TestApplyWaiverWhitespaceMissingReqSavedUnderApplicant(t *testing.T) {
	d := &fileData{
		Version:      recordVersion,
		Courses:      []*Course{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
	}
	for _, reqID := range []string{" r1", "r1 ", " r1 ", "\tr1\t", "　r1　"} {
		s := mustLoadWaiverWhitespace(t, d)
		w, a, err := s.ApplyWaiver("s1", reqID, " w1 ", "实际依据文字")
		if err != nil || a != ActionCreated || w.Status != WaiverRejected {
			t.Fatalf("要求 %q 不存在应业务拒绝并留历史，得到 %+v action=%v err=%v",
				reqID, w, a, err)
		}
		if w.StudentID != "s1" || w.ReqID != reqID || w.ID != " w1 " ||
			w.Basis != "实际依据文字" || !strings.Contains(w.Reason, "不存在或不属于该学生") {
			t.Fatalf("拒绝记录应保留完整的要求编号 %q、免修编号与原因，得到 %+v", reqID, w)
		}
		// 不借用去空白后碰上的 r1：r1 仍未满足、学分为零。
		rep := s.CheckStudent("s1")
		if rep.TotalCredits != 0 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
			t.Fatalf("要求 %q 的被拒绝申请不应满足 r1，得到 学分=%d 未满足=%v",
				reqID, rep.TotalCredits, rep.Unmet)
		}
		if len(rep.RejectedWaivers) != 1 {
			t.Fatalf("申请人名下应保留 1 条拒绝记录，得到 %+v", rep.RejectedWaivers)
		}
		rj := rep.RejectedWaivers[0]
		if rj.Waiver != w || rj.Waiver.ReqID != reqID || rj.Waiver.ID != " w1 " {
			t.Fatalf("核对中的拒绝记录应保留完整编号，得到 %+v", rj)
		}
		if w := s.validWaiver("s1", "r1"); w != nil {
			t.Fatalf("r1 不应因该申请获得有效免修，得到 %+v", w)
		}
	}
}

// TestApplyWaiverWhitespaceMissingReqDoesNotTouchOtherStudent 同号要求只
// 属于另一名学生（编号文字也完全一致）时，申请人的拒绝历史只记在本人
// 名下，对方的要求、学分与历史保持原样。
func TestApplyWaiverWhitespaceMissingReqDoesNotTouchOtherStudent(t *testing.T) {
	s := mustLoadWaiverWhitespace(t, waiverWhitespaceBase())
	// " s1 " 先凭本人 r1 取得有效免修（3 学分）。
	prior, a, err := s.ApplyWaiver(" s1 ", "r1", "w1", " s1 的获奖依据")
	if err != nil || a != ActionCreated || prior.Status != WaiverApproved {
		t.Fatalf("\" s1 \" 的前置申请应生效，得到 %+v err=%v", prior, err)
	}

	// "s1" 名下没有编号为 " r1 " 的要求（其要求是不带空白的 r1）：
	// 即使去掉空白能命中本人的 r1，也必须按不存在拒绝并留本人历史。
	w, a, err := s.ApplyWaiver("s1", " r1 ", "wx", "s1 的材料")
	if err != nil || a != ActionCreated || w.Status != WaiverRejected {
		t.Fatalf("\"s1\" 申请不存在的 \" r1 \" 应拒绝留历史，得到 %+v action=%v err=%v",
			w, a, err)
	}
	if w.StudentID != "s1" || w.ReqID != " r1 " || w.ID != "wx" {
		t.Fatalf("拒绝记录应完整保留在 s1 名下，得到 %+v", w)
	}

	// " s1 " 的有效免修与 3 学分保持原样，历史中不出现 s1 的申请。
	if got := s.Waiver(" s1 ", "w1"); got != prior || got.Status != WaiverApproved {
		t.Fatalf("\" s1 \" 的 w1 不应受影响，得到 %+v", got)
	}
	if rep := s.CheckStudent(" s1 "); rep.TotalCredits != 3 ||
		len(rep.RejectedWaivers) != 0 || rep.Requirements[0].WaiverID != "w1" {
		t.Fatalf("\" s1 \" 应仍为 3 学分且由 w1 满足，得到 %+v", rep)
	}
	if ws := s.Waivers(" s1 "); len(ws) != 1 || ws[0] != prior {
		t.Fatalf("\" s1 \" 的历史不应混入 s1 的拒绝记录，得到 %+v", ws)
	}
	// "s1" 本人：0 学分、r1 未满足，拒绝记录可查。
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 0 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("\"s1\" 应仍为 0 学分、r1 未满足，得到 %+v", rep)
	}
	if len(rep.RejectedWaivers) != 1 || rep.RejectedWaivers[0].Waiver != w {
		t.Fatalf("s1 的核对中应只列出本人的拒绝记录，得到 %+v", rep.RejectedWaivers)
	}
}

// TestApplyWaiverEmptyIDsRejected 只有空字符串编号按“不能为空”拒绝，
// 不创建任何申请；全空白编号不是空字符串，仍按完整编号参与查找。
func TestApplyWaiverEmptyIDsRejected(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "数学", 4)
	mustReq(t, s, "s1", "r1", "c1")

	for _, tc := range [][]string{
		{"", "r1", "w1", "依据"},
		{"s1", "", "w1", "依据"},
		{"s1", "r1", "", "依据"},
	} {
		if w, _, err := s.ApplyWaiver(tc[0], tc[1], tc[2], tc[3]); err == nil {
			t.Fatalf("编号 %v 含空字符串应拒绝，却得到 %+v", tc, w)
		} else if !strings.Contains(err.Error(), "不能为空") {
			t.Fatalf("编号 %v 应报不能为空，得到 %v", tc, err)
		}
		if ws := s.Waivers("s1"); len(ws) != 0 {
			t.Fatalf("空编号拒绝不应留下任何申请（%v），得到 %+v", tc, ws)
		}
	}

	// 全空白编号不是空字符串：按完整编号查找，学生不存在时直接拒绝。
	if _, _, err := s.ApplyWaiver(" ", "r1", "w1", "依据"); err == nil ||
		!strings.Contains(err.Error(), "不存在") {
		t.Fatalf("全空白学生编号应按完整编号查找并报不存在，得到 %v", err)
	}
}

// TestApplyWaiverBasisWhitespacePreserved 依据不修剪：含实际文字时前后
// 与中间空白原样保存并参与内容比较；全部为空白时仍按“免修依据为空”
// 业务拒绝，拒绝记录中的依据原文同样保留。
func TestApplyWaiverBasisWhitespacePreserved(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "数学", 4)
	mustReq(t, s, "s1", "r1", "c1")
	mustCourse(t, s, "c2", "物理", 3)
	mustReq(t, s, "s1", "r2", "c2")

	basis := "\t  学科竞赛获奖证明　\n  "
	w, a, err := s.ApplyWaiver("s1", "r1", "w1", basis)
	if err != nil || a != ActionCreated || w.Status != WaiverApproved {
		t.Fatalf("含实际文字的依据应正常生效，得到 %+v action=%v err=%v", w, a, err)
	}
	if w.Basis != basis {
		t.Fatalf("依据的前后与中间空白必须原样保存，\nwant=%q\n got=%q", basis, w.Basis)
	}

	// 原样重试（含同样的前后空白）幂等返回。
	if got, a, err := s.ApplyWaiver("s1", "r1", "w1", basis); err != nil ||
		a != ActionExisted || got != w {
		t.Fatalf("原样重试应幂等返回原申请，得到 %+v action=%v err=%v", got, a, err)
	}
	// 只去掉前后空白也算不同内容：冲突拒绝，原依据保留。
	if _, _, err := s.ApplyWaiver("s1", "r1", "w1", "学科竞赛获奖证明"); err == nil {
		t.Fatal("依据前后空白不同应按内容冲突拒绝")
	}
	if got := s.Waiver("s1", "w1"); got.Basis != basis || got.Status != WaiverApproved {
		t.Fatalf("冲突拒绝后原依据应逐字符保留，得到 %+v", got)
	}

	// 纯空白依据：业务拒绝，拒绝历史中的依据原文不修剪。
	blank := " 　\t \n "
	wb, a, err := s.ApplyWaiver("s1", "r2", "w2", blank)
	if err != nil || a != ActionCreated || wb.Status != WaiverRejected ||
		wb.Reason != "免修依据为空" {
		t.Fatalf("纯空白依据应业务拒绝，得到 %+v action=%v err=%v", wb, a, err)
	}
	if wb.Basis != blank {
		t.Fatalf("被拒绝申请的依据原文也应原样保留，\nwant=%q\n got=%q", blank, wb.Basis)
	}

	// 保存重新打开：两份依据均逐字符保留，文件合法。
	path := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("含带空白依据的文件应正常打开：%v", err)
	}
	if got := reloaded.Waiver("s1", "w1"); got == nil || got.Basis != basis {
		t.Fatalf("重新打开后 w1 的依据原文应不变，得到 %+v", got)
	}
	if got := reloaded.Waiver("s1", "w2"); got == nil || got.Basis != blank ||
		got.Status != WaiverRejected {
		t.Fatalf("重新打开后 w2 的空白依据与拒绝状态应保留，得到 %+v", got)
	}
}

// TestApplyWaiverWhitespaceExistingRulesPreserved 准确命中带空白编号的
// 申请后，现有规则保持不变：同内容返回原结果、内容冲突拒绝修改；已撤销
// 的申请重试不重新生效；新编号申请已有有效免修的要求只新增一条拒绝记录。
func TestApplyWaiverWhitespaceExistingRulesPreserved(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s1"}},
		Requirements: []*Requirement{
			{ID: " r1 ", StudentID: "s1", CourseID: "c2"},
		},
	}
	s := mustLoadWaiverWhitespace(t, d)

	w, a, err := s.ApplyWaiver("s1", " r1 ", " w1 ", "外校修读证明")
	if err != nil || a != ActionCreated || w.Status != WaiverApproved {
		t.Fatalf("首次申请应生效，得到 %+v action=%v err=%v", w, a, err)
	}
	// 同内容重试：返回原有效申请。
	if got, a, err := s.ApplyWaiver("s1", " r1 ", " w1 ", "外校修读证明"); err != nil ||
		a != ActionExisted || got != w || got.Status != WaiverApproved {
		t.Fatalf("同内容重试应返回原有效申请，得到 %+v action=%v err=%v", got, a, err)
	}
	// 撤销后重试：仍是已撤销状态，不重新生效。
	if rw, changed, err := s.RevokeWaiver("s1", " w1 ", "材料无法核实"); err != nil ||
		!changed || rw.Status != WaiverRevoked {
		t.Fatalf("撤销应成功，得到 %+v changed=%v err=%v", rw, changed, err)
	}
	if got, a, err := s.ApplyWaiver("s1", " r1 ", " w1 ", "外校修读证明"); err != nil ||
		a != ActionExisted || got != w || got.Status != WaiverRevoked {
		t.Fatalf("重试已撤销申请不应使其重新生效，得到 %+v action=%v err=%v", got, a, err)
	}
	if rep := s.CheckStudent("s1"); rep.TotalCredits != 0 ||
		len(rep.RevokedWaivers) != 1 || rep.RevokedWaivers[0] != " w1 " {
		t.Fatalf("撤销后要求应未满足并列出已撤销的 \" w1 \"，得到 %+v", rep)
	}

	// 新免修编号申请同一要求：该要求当前没有有效免修，应正常生效；
	// 再用第三个编号申请则只留下一条被拒绝的新申请，不产生第二份有效免修。
	w2, a, err := s.ApplyWaiver("s1", " r1 ", " w2 ", "补充的获奖材料")
	if err != nil || a != ActionCreated || w2.Status != WaiverApproved {
		t.Fatalf("撤销后新编号申请应正常生效，得到 %+v action=%v err=%v", w2, a, err)
	}
	w3, a, err := s.ApplyWaiver("s1", " r1 ", " w3 ", "再次申请")
	if err != nil || a != ActionCreated || w3.Status != WaiverRejected {
		t.Fatalf("已有有效免修时新编号申请应只留拒绝记录，得到 %+v action=%v err=%v",
			w3, a, err)
	}
	if !strings.Contains(w3.Reason, " w2 ") {
		t.Fatalf("拒绝原因应点名当前有效免修 \" w2 \"，得到 %q", w3.Reason)
	}
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 3 || rep.Requirements[0].WaiverID != " w2 " {
		t.Fatalf("学分应只计一份、来源为 \" w2 \"，得到 %+v", rep)
	}
}
