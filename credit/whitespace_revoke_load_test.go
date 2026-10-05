package credit

import (
	"strings"
	"testing"
)

// 本文件为“撤销免修时按用户给出的完整学生编号与免修编号确定撤销目标”
// 这项行为提供回归保障。记录文件可以合法保存带前后空白的学生编号与免修
// 编号（申请入口会修剪编号，但既有文件中的编号原文不得改写，Load 也按
// 原样接受），因此全部用例直接构造记录文件，聚焦 revoke-waiver 的选对象
// 逻辑：
//   - “s1”与“ s1 ”是两名不同学生，同一学生名下“w1”与“ w1 ”是两份
//     不同免修；普通空格、制表符、全角空格（U+3000）都是编号内容；
//   - 撤销只命中编号完全一致的那一份，另一名学生或同一名学生名下另一份
//     免修的状态、学分、来源保持原样；
//   - 完整编号找不到学生时明确报学生不存在，学生存在但名下没有该完整
//     免修编号时明确报名下不存在该免修；即使去掉空白能碰上另一条记录，
//     也绝不借用那份记录，也不能改写编号、合并申请或新增免修历史；
//   - 含前后空白的编号本身合法，绝不能判为文件损坏；
//   - 准确命中后沿用现有学分规则与撤销规则：无通过修读时要求重新未满足，
//     有通过修读时仍只得一份课程学分且来源恢复为最先提交的通过记录；
//     重复撤销返回原结果且首因不被覆盖；已拒绝申请不能撤销。

// revokeWhitespaceBase 构造结构完整、引用齐全的合法记录：
// 学生 "s1" 与 " s1 "（前后普通空格）各自的要求 r1 指向 4 学分课程 c1，
// 名下各有一份有效免修 w1，均无通过修读。
func revokeWhitespaceBase() *fileData {
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
		Waivers: []*Waiver{
			{ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "s1 的竞赛获奖", Status: WaiverApproved},
			{ID: "w1", StudentID: " s1 ", ReqID: "r1", Basis: " s1 的竞赛材料 ", Status: WaiverApproved},
		},
	}
}

// mustLoadRevokeWhitespace 加载合法记录；含前后空白编号的记录必须被接受，
// 不能判为文件损坏。
func mustLoadRevokeWhitespace(t *testing.T, d *fileData) *Store {
	t.Helper()
	path, _ := writeRecord(t, d)
	s, _, err := Load(path)
	if err != nil {
		t.Fatalf("含前后空白编号的记录是合法记录，不应判为损坏：%v", err)
	}
	return s
}

// TestRevokeWhitespaceStudentIDsExact 两名学生 "s1" 与 " s1 " 各有一份
// 有效免修 w1：指定 " s1 " 撤销 w1 只撤销这名学生的申请；其要求重新未
// 满足、失去这份免修的学分，免修历史保留原要求与原依据并记录撤销状态
// 与原因；"s1" 的免修状态、学分和来源保持原样。
func TestRevokeWhitespaceStudentIDsExact(t *testing.T) {
	s := mustLoadRevokeWhitespace(t, revokeWhitespaceBase())

	w, changed, err := s.RevokeWaiver(" s1 ", "w1", "材料无法核实")
	if err != nil || !changed {
		t.Fatalf("撤销 \" s1 \" 的 w1 应成功，changed=%v err=%v", changed, err)
	}
	// 撤销提示必须使用实际命中的原编号与原归属，不能变成修剪后的文字。
	if w.StudentID != " s1 " || w.ID != "w1" || w.ReqID != "r1" ||
		w.Basis != " s1 的竞赛材料 " {
		t.Fatalf("命中的应是 \" s1 \" 本人的 w1 且原要求、原依据保留，得到 %+v", w)
	}
	if w.Status != WaiverRevoked || w.Reason != "材料无法核实" {
		t.Fatalf("应记录撤销状态与本次原因，得到 %+v", w)
	}

	// " s1 " 本人：无通过修读，要求重新未满足、0 学分，核对应列出已撤销 w1。
	rep := s.CheckStudent(" s1 ")
	if !rep.Found || rep.TotalCredits != 0 {
		t.Fatalf("\" s1 \" 撤销后应失去这份免修学分，得到 %+v", rep)
	}
	if len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("\" s1 \" 的 r1 应重新列为未满足，得到 %v", rep.Unmet)
	}
	if rep.Requirements[0].Source != "" || rep.Requirements[0].WaiverID != "" {
		t.Fatalf("\" s1 \" 的 r1 不应再有免修来源，得到 %+v", rep.Requirements[0])
	}
	if len(rep.RevokedWaivers) != 1 || rep.RevokedWaivers[0] != "w1" {
		t.Fatalf("\" s1 \" 的核对应列出已撤销免修 w1，得到 %v", rep.RevokedWaivers)
	}

	// "s1" 保持原样：w1 仍有效，r1 由本人免修满足、4 学分。
	if got := s.Waiver("s1", "w1"); got == nil || got.Status != WaiverApproved ||
		got.Basis != "s1 的竞赛获奖" {
		t.Fatalf("\"s1\" 的 w1 不应被改写，得到 %+v", got)
	}
	rep = s.CheckStudent("s1")
	if !rep.Found || rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
		t.Fatalf("\"s1\" 应仍凭本人 w1 获得 4 学分，得到 %+v", rep)
	}
	if len(rep.Requirements) != 1 || rep.Requirements[0].Source != "waiver" ||
		rep.Requirements[0].WaiverID != "w1" {
		t.Fatalf("\"s1\" 的来源应仍是本人有效免修 w1，得到 %+v", rep.Requirements)
	}
	if len(rep.RevokedWaivers) != 0 {
		t.Fatalf("\"s1\" 名下不应出现撤销历史，得到 %v", rep.RevokedWaivers)
	}
}

// TestRevokeWhitespaceWaiverIDsExact 同一学生名下 "w1" 与 " w1 " 两份
// 有效免修分别指向不同要求（r1 与 r2）：指定 " w1 " 只能撤销带空格的
// 那份，"w1" 继续有效，另一项要求与其学分不受影响。
func TestRevokeWhitespaceWaiverIDsExact(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s1"}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: "c1"},
			{ID: "r2", StudentID: "s1", CourseID: "c2"},
		},
		Waivers: []*Waiver{
			{ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "r1 的竞赛获奖", Status: WaiverApproved},
			{ID: " w1 ", StudentID: "s1", ReqID: "r2", Basis: " r2 的外校修读 ", Status: WaiverApproved},
		},
	}
	s := mustLoadRevokeWhitespace(t, d)

	w, changed, err := s.RevokeWaiver("s1", " w1 ", "材料无法核实")
	if err != nil || !changed {
		t.Fatalf("撤销 \" w1 \" 应成功，changed=%v err=%v", changed, err)
	}
	// 命中的必须是带空格的原编号，且保留其原有要求与依据。
	if w.ID != " w1 " || w.ReqID != "r2" || w.Basis != " r2 的外校修读 " ||
		w.Status != WaiverRevoked || w.Reason != "材料无法核实" {
		t.Fatalf("应只撤销带空格的 \" w1 \" 并保留原要求、原依据，得到 %+v", w)
	}

	// r2（" w1 " 所在要求）无通过修读：重新未满足，3 学分消失。
	if got := s.Waiver("s1", " w1 "); got == nil || got.Status != WaiverRevoked {
		t.Fatalf("\" w1 \" 应为已撤销，得到 %+v", got)
	}
	// r1 的 "w1" 继续有效：仍满足、4 学分、来源还是本人有效免修 w1。
	if got := s.Waiver("s1", "w1"); got == nil || got.Status != WaiverApproved {
		t.Fatalf("不带空格的 \"w1\" 应继续有效，得到 %+v", got)
	}
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 4 {
		t.Fatalf("撤销 \" w1 \" 后应只剩 r1 的 4 学分，得到 %d（%+v）", rep.TotalCredits, rep)
	}
	if len(rep.Unmet) != 1 || rep.Unmet[0] != "r2" {
		t.Fatalf("只有 r2 应重新列为未满足，得到 %v", rep.Unmet)
	}
	stByReq := map[string]RequirementStatus{}
	for _, st := range rep.Requirements {
		stByReq[st.Req.ID] = st
	}
	if stByReq["r1"].Source != "waiver" || stByReq["r1"].WaiverID != "w1" ||
		!stByReq["r1"].Satisfied {
		t.Fatalf("r1 应仍由有效免修 w1 满足，得到 %+v", stByReq["r1"])
	}
	if stByReq["r2"].Satisfied || stByReq["r2"].Source != "" {
		t.Fatalf("r2 应重新未满足且无来源，得到 %+v", stByReq["r2"])
	}
	if len(rep.RevokedWaivers) != 1 || rep.RevokedWaivers[0] != " w1 " {
		t.Fatalf("已撤销列表应保留原编号 \" w1 \"，得到 %v", rep.RevokedWaivers)
	}
}

// TestRevokeWhitespaceRestoresEarliestPass 命中带空白编号的免修后沿用
// 现有学分规则：目标要求已有通过修读时仍只得一份课程学分，来源恢复为
// 最先提交的通过记录；没有通过修读的另一项要求与另一名学生不受影响。
func TestRevokeWhitespaceRestoresEarliestPass(t *testing.T) {
	d := revokeWhitespaceBase()
	// " s1 " 的 r1 有两次通过：e1 先提交、e2 后提交。撤销免修后来源应
	// 恢复为最先提交的 e1，4 学分仍只计一份。
	d.Enrollments = []*Enrollment{
		{ID: "e1", StudentID: " s1 ", ReqID: "r1", Term: "2024春", Result: Passed, ResultSeq: 1},
		{ID: "e2", StudentID: " s1 ", ReqID: "r1", Term: "2024秋", Result: Passed, ResultSeq: 2},
	}
	d.NextResultSeq = 2
	s := mustLoadRevokeWhitespace(t, d)

	if _, changed, err := s.RevokeWaiver(" s1 ", "w1", "材料无法核实"); err != nil || !changed {
		t.Fatalf("撤销 \" s1 \" 的 w1 应成功，changed=%v err=%v", changed, err)
	}
	rep := s.CheckStudent(" s1 ")
	if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
		t.Fatalf("有通过修读时撤销免修应继续满足、仍只计 4 学分，得到 学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	st := rep.Requirements[0]
	if st.Source != "enrollment" || st.PassedEnrollmentID != "e1" {
		t.Fatalf("来源应恢复为最先提交的通过记录 e1，得到 %+v", st)
	}
	if len(st.PassedEnrollmentIDs) != 2 || st.PassedEnrollmentIDs[0] != "e1" ||
		st.PassedEnrollmentIDs[1] != "e2" {
		t.Fatalf("两次通过历史应完整保留且按提交先后排列，得到 %v", st.PassedEnrollmentIDs)
	}
	// "s1" 的免修与学分完全不受影响。
	if got := s.Waiver("s1", "w1"); got == nil || got.Status != WaiverApproved {
		t.Fatalf("\"s1\" 的 w1 应继续有效，得到 %+v", got)
	}
	if rep := s.CheckStudent("s1"); rep.TotalCredits != 4 ||
		rep.Requirements[0].Source != "waiver" {
		t.Fatalf("\"s1\" 的学分与来源应保持原样，得到 %+v", rep.Requirements)
	}
}

// TestRevokeWhitespaceNoBorrow 完整编号找不到时必须明确拒绝：即使去掉
// 空白后能碰上另一名学生或另一份免修，也绝不借用那份记录，不能改写
// 编号、合并申请或新增免修历史。普通空格、制表符、全角空格一样不能被
// 忽略；被拒绝的撤销不产生任何变更。
func TestRevokeWhitespaceNoBorrow(t *testing.T) {
	// 情形一：文件中只有学生 "s1" 且名下有有效免修 w1。
	onlyS1 := &fileData{
		Version:      recordVersion,
		Courses:      []*Course{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
		Waivers: []*Waiver{
			{ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "竞赛获奖", Status: WaiverApproved},
		},
	}

	// 学生编号带各种前后空白：文件中只有 "s1"，都应报学生不存在。
	for _, student := range []string{" s1", "s1 ", " s1 ", "\ts1\t", "　s1　"} {
		s := mustLoadRevokeWhitespace(t, onlyS1)
		_, _, err := s.RevokeWaiver(student, "w1", "原因")
		if err == nil || !strings.Contains(err.Error(), "不存在") {
			t.Fatalf("学生 %q 不存在，撤销应明确拒绝，得到 %v", student, err)
		}
		if !strings.Contains(err.Error(), "学生") {
			t.Fatalf("学生 %q 找不到时应明确报告学生不存在，得到 %v", student, err)
		}
		// 不能借用 "s1" 的免修，也不能新增任何历史。
		if got := s.Waiver("s1", "w1"); got == nil || got.Status != WaiverApproved {
			t.Fatalf("学生 %q 被拒绝后 \"s1\" 的 w1 不应变化，得到 %+v", student, got)
		}
		if ws := s.Waivers("s1"); len(ws) != 1 {
			t.Fatalf("不应为被拒绝的撤销新增免修历史（学生 %q）：%+v", student, ws)
		}
		if s.Dirty() {
			t.Fatalf("被拒绝的撤销不应产生变更（学生 %q）", student)
		}
	}

	// 情形二：学生 "s1" 存在但只有不带空格的 w1：免修编号带各种前后
	// 空白时都应报告该学生名下不存在该免修。
	for _, waiverID := range []string{" w1", "w1 ", " w1 ", "\tw1\t", "　w1　"} {
		s := mustLoadRevokeWhitespace(t, onlyS1)
		_, _, err := s.RevokeWaiver("s1", waiverID, "原因")
		if err == nil || !strings.Contains(err.Error(), "不存在") {
			t.Fatalf("免修 %q 不存在于 s1 名下，撤销应明确拒绝，得到 %v", waiverID, err)
		}
		if !strings.Contains(err.Error(), "s1") {
			t.Fatalf("错误应点名学生 s1 名下不存在该免修，得到 %v", err)
		}
		if got := s.Waiver("s1", "w1"); got == nil || got.Status != WaiverApproved {
			t.Fatalf("免修 %q 被拒绝后 \"w1\" 不应变化，得到 %+v", waiverID, got)
		}
		if ws := s.Waivers("s1"); len(ws) != 1 || ws[0].ID != "w1" {
			t.Fatalf("不应补建或合并免修 %q，历史应仍只有 w1：%+v", waiverID, ws)
		}
		if s.Dirty() {
			t.Fatalf("被拒绝的撤销不应产生变更（免修 %q）", waiverID)
		}
	}

	// 情形三：名下是 " w1 " 的学生去撤销 "w1"，以及反过来，都应按不存在
	// 拒绝，另一份申请继续有效。
	twoWaivers := &fileData{
		Version:      recordVersion,
		Courses:      []*Course{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
		Waivers: []*Waiver{
			{ID: " w1 ", StudentID: "s1", ReqID: "r1", Basis: "带空格编号的依据", Status: WaiverApproved},
		},
	}
	s := mustLoadRevokeWhitespace(t, twoWaivers)
	if _, _, err := s.RevokeWaiver("s1", "w1", "原因"); err == nil ||
		!strings.Contains(err.Error(), "名下不存在免修 w1") {
		t.Fatalf("只有 \" w1 \" 时撤销 \"w1\" 应按不存在拒绝，得到 %v", err)
	}
	if got := s.Waiver("s1", " w1 "); got == nil || got.Status != WaiverApproved {
		t.Fatalf("被拒绝后 \" w1 \" 应继续有效，得到 %+v", got)
	}
	if s.Dirty() {
		t.Fatal("按不存在拒绝不应产生变更")
	}
}

// TestRevokeWhitespaceIdempotentKeepsFirstReason 准确命中带空白编号的
// 已撤销免修后重复撤销：返回同一条原记录、changed=false，后来给出的
// 原因不会覆盖首次撤销原因，也不产生新变更。
func TestRevokeWhitespaceIdempotentKeepsFirstReason(t *testing.T) {
	s := mustLoadRevokeWhitespace(t, revokeWhitespaceBase())

	first, changed, err := s.RevokeWaiver(" s1 ", "w1", "首次撤销：材料存疑")
	if err != nil || !changed {
		t.Fatalf("首次撤销应成功，changed=%v err=%v", changed, err)
	}
	s.dirty = false // 只看后续撤销是否产生新变更

	for _, reason := range []string{"第二次原因", "  ", ""} {
		got, changed, err := s.RevokeWaiver(" s1 ", "w1", reason)
		if err != nil || changed || got != first {
			t.Fatalf("重复撤销应幂等返回原记录，reason=%q changed=%v err=%v got=%+v",
				reason, changed, err, got)
		}
		if got.ID != "w1" || got.StudentID != " s1 " ||
			got.Reason != "首次撤销：材料存疑" || got.Status != WaiverRevoked {
			t.Fatalf("首因与原编号不应被覆盖，得到 %+v", got)
		}
	}
	if s.Dirty() {
		t.Fatal("幂等重复撤销不应产生变更")
	}
	// 免修历史仍是一条，原要求与原依据保留。
	ws := s.Waivers(" s1 ")
	if len(ws) != 1 || ws[0].ReqID != "r1" || ws[0].Basis != " s1 的竞赛材料 " {
		t.Fatalf("免修历史应保留原要求与原依据且不新增，得到 %+v", ws)
	}
	// "s1" 的 w1 始终不受影响。
	if got := s.Waiver("s1", "w1"); got == nil || got.Status != WaiverApproved {
		t.Fatalf("\"s1\" 的 w1 应继续有效，得到 %+v", got)
	}
}

// TestRevokeWhitespaceRejectedCannotRevoke 已拒绝的申请即使编号带前后
// 空白也不能撤销：按业务规则拒绝，拒绝状态与原因原样保留，另一份有效
// 免修与其他记录不受影响。
func TestRevokeWhitespaceRejectedCannotRevoke(t *testing.T) {
	d := &fileData{
		Version:      recordVersion,
		Courses:      []*Course{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []*Student{{ID: "s1"}, {ID: " s1 "}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
		Waivers: []*Waiver{
			// " s1 " 的 " w1 " 因目标要求不存在已被拒绝（历史允许如此）。
			{ID: " w1 ", StudentID: " s1 ", ReqID: "rX", Basis: "依据",
				Status: WaiverRejected, Reason: "目标要求 rX 不存在或不属于该学生"},
			// "s1" 本人另有一份有效 w1，必须不受影响。
			{ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "s1 的依据", Status: WaiverApproved},
		},
	}
	s := mustLoadRevokeWhitespace(t, d)

	_, _, err := s.RevokeWaiver(" s1 ", " w1 ", "新的撤销原因")
	if err == nil || !strings.Contains(err.Error(), "已被拒绝") {
		t.Fatalf("已拒绝的申请不能撤销，应明确拒绝，得到 %v", err)
	}
	got := s.Waiver(" s1 ", " w1 ")
	if got == nil || got.Status != WaiverRejected ||
		got.Reason != "目标要求 rX 不存在或不属于该学生" || got.Basis != "依据" {
		t.Fatalf("拒绝后原状态、首因与依据应原样保留，得到 %+v", got)
	}
	if s.Dirty() {
		t.Fatal("拒绝撤销已拒绝申请不应产生变更")
	}
	if got := s.Waiver("s1", "w1"); got == nil || got.Status != WaiverApproved {
		t.Fatalf("\"s1\" 的有效 w1 不应受影响，得到 %+v", got)
	}

	// 修剪后能碰上 "s1" 的 w1，也绝不能借用：用 "s1 " 等编号撤销必须按
	// 学生不存在拒绝，"s1" 的有效免修保持原样。
	for _, student := range []string{" s1", "s1 ", " s1 ", "\ts1\t", "　s1　"} {
		_, _, err := s.RevokeWaiver(student, "w1", "原因")
		if err == nil || !strings.Contains(err.Error(), "学生") ||
			!strings.Contains(err.Error(), "不存在") {
			t.Fatalf("学生 %q 不存在应明确拒绝，得到 %v", student, err)
		}
	}
	if got := s.Waiver("s1", "w1"); got.Status != WaiverApproved {
		t.Fatalf("借用被全部拒绝后 \"s1\" 的 w1 应仍有效，得到 %+v", got)
	}
}
