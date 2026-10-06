package credit

import (
	"strings"
	"testing"
)

// 本文件为“撤销免修时按用户给出的完整编号确定撤销目标”这项行为提供
// 回归保障。记录文件可以合法保存带前后空白的学生编号与免修编号（登记
// 入口同样保留完整编号，既有文件中的编号原文不得改写，Load 也按原样
// 接受），因此全部用例直接构造记录文件，聚焦 revoke-waiver 的选对象逻辑：
//   - “s1”与“ s1 ”是两名不同学生，同一学生名下“w1”与“ w1 ”是两份
//     不同免修；普通空格、制表符、全角空格（U+3000）都是编号内容；
//   - 给带空白的编号撤销只命中编号完全一致的那一份，另一名学生或
//     另一份免修的状态、学分与来源保持原样；
//   - 完整编号找不到学生时明确报学生不存在，学生存在但名下没有该完整
//     免修编号时明确报名下不存在该免修；即使去掉空白能碰上另一条记录，
//     也绝不借用那份记录，不改写编号、不合并申请、不新增免修历史；
//   - 含前后空白的编号本身合法，绝不能判为文件损坏；
//   - 准确命中后沿用现有规则：无通过记录则要求重新未满足并失去学分，
//     有通过记录仍只得一份学分且来源恢复为最先提交的通过记录；重复
//     撤销返回原结果且首因不被覆盖；已拒绝申请仍不能撤销。

// revokeWhitespaceTwoStudents 构造两名学生 "s1" 与 " s1 " 各有一份编号
// 为 w1 的有效免修（各自的要求 r1 都指向 4 学分课程 c1），两人都没有
// 任何修读。
func revokeWhitespaceTwoStudents() *fileData {
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
			{ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "s1的获奖证明", Status: WaiverApproved},
			{ID: "w1", StudentID: " s1 ", ReqID: "r1", Basis: " s1 的获奖证明", Status: WaiverApproved},
		},
	}
}

// TestRevokeWaiverWhitespaceStudentIDExact 两名学生 "s1" 与 " s1 " 各有
// 一份有效免修 w1：撤销 " s1 " 的 w1 只命中本人的申请，本人的要求重新
// 未满足、失去学分、免修历史保留原要求与依据并记录撤销状态与原因；
// "s1" 的免修状态、学分和来源保持原样。
func TestRevokeWaiverWhitespaceStudentIDExact(t *testing.T) {
	s, _ := mustLoadWhitespace(t, revokeWhitespaceTwoStudents())

	w, changed, err := s.RevokeWaiver(" s1 ", "w1", "材料无法核实")
	if err != nil || !changed {
		t.Fatalf("撤销 \" s1 \" 的 w1 应成功，changed=%v err=%v", changed, err)
	}
	if w.StudentID != " s1 " || w.ID != "w1" {
		t.Fatalf("命中的应是 \" s1 \" 本人的 w1，得到 %+v", w)
	}
	if w.Status != WaiverRevoked || w.ReqID != "r1" ||
		w.Basis != " s1 的获奖证明" || w.Reason != "材料无法核实" {
		t.Fatalf("免修历史应保留原要求、原依据并记录撤销状态与原因，得到 %+v", w)
	}

	// " s1 " 本人：没有通过修读，要求重新未满足、失去这份免修的 4 学分。
	rep := s.CheckStudent(" s1 ")
	if !rep.Found || rep.TotalCredits != 0 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("\" s1 \" 撤销后应回到 0 学分且 r1 未满足，得到 %+v", rep)
	}
	if st := rep.Requirements[0]; st.Satisfied || st.Source != "" || st.WaiverID != "" {
		t.Fatalf("\" s1 \" 的 r1 不应再有免修来源，得到 %+v", st)
	}
	if len(rep.RevokedWaivers) != 1 || rep.RevokedWaivers[0] != "w1" {
		t.Fatalf("核对应列出 \" s1 \" 的已撤销免修 w1，得到 %v", rep.RevokedWaivers)
	}

	// "s1" 保持原样：w1 仍有效、r1 满足、4 学分且来源是本人的有效免修。
	if got := s.Waiver("s1", "w1"); got == nil || got.Status != WaiverApproved ||
		got.Basis != "s1的获奖证明" {
		t.Fatalf("\"s1\" 的 w1 不应被改写，得到 %+v", got)
	}
	rep = s.CheckStudent("s1")
	if !rep.Found || rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
		t.Fatalf("\"s1\" 应仍为 4 学分且 r1 已满足，得到 %+v", rep)
	}
	if st := rep.Requirements[0]; !st.Satisfied || st.Source != "waiver" || st.WaiverID != "w1" {
		t.Fatalf("\"s1\" 的来源应仍是本人有效免修 w1，得到 %+v", st)
	}
	if len(rep.RevokedWaivers) != 0 {
		t.Fatalf("\"s1\" 不应出现已撤销免修，得到 %v", rep.RevokedWaivers)
	}
}

// TestRevokeWaiverWhitespaceWaiverIDExact 同一学生名下 "w1" 与 " w1 " 是
// 指向不同要求的两份有效免修：撤销 " w1 " 只处理带空格的那份（其要求
// 重新未满足、失去相应学分），"w1" 继续有效，另一份要求的满足情况、
// 学分与来源不受影响。
func TestRevokeWaiverWhitespaceWaiverIDExact(t *testing.T) {
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
			{ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "竞赛获奖", Status: WaiverApproved},
			{ID: " w1 ", StudentID: "s1", ReqID: "r2", Basis: "外校修读证明", Status: WaiverApproved},
		},
	}
	s, _ := mustLoadWhitespace(t, d)

	w, changed, err := s.RevokeWaiver("s1", " w1 ", "材料无法核实")
	if err != nil || !changed {
		t.Fatalf("撤销 \" w1 \" 应作为独立免修被命中，changed=%v err=%v", changed, err)
	}
	if w.ID != " w1 " || w.StudentID != "s1" || w.ReqID != "r2" ||
		w.Basis != "外校修读证明" || w.Status != WaiverRevoked || w.Reason != "材料无法核实" {
		t.Fatalf("命中的应是 \" w1 \" 本身且原要求、原依据保留，得到 %+v", w)
	}

	// r2 失去免修重新未满足，总学分只剩 r1 的 4 学分，来源仍是 w1。
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 4 {
		t.Fatalf("撤销 \" w1 \" 后应只剩 r1 的 4 学分，得到 %d", rep.TotalCredits)
	}
	if len(rep.Unmet) != 1 || rep.Unmet[0] != "r2" {
		t.Fatalf("应只有 r2 重新未满足，得到 %v", rep.Unmet)
	}
	statuses := map[string]RequirementStatus{}
	for _, st := range rep.Requirements {
		statuses[st.Req.ID] = st
	}
	if st := statuses["r1"]; !st.Satisfied || st.Source != "waiver" || st.WaiverID != "w1" {
		t.Fatalf("r1 应继续由有效免修 w1 满足，得到 %+v", st)
	}
	if st := statuses["r2"]; st.Satisfied || st.Source != "" {
		t.Fatalf("r2 应重新未满足，得到 %+v", st)
	}
	if len(rep.RevokedWaivers) != 1 || rep.RevokedWaivers[0] != " w1 " {
		t.Fatalf("已撤销列表应只含 \" w1 \"，得到 %v", rep.RevokedWaivers)
	}

	// 不带空格的 w1 继续有效。
	if got := s.Waiver("s1", "w1"); got == nil || got.Status != WaiverApproved {
		t.Fatalf("\"w1\" 应继续有效，得到 %+v", got)
	}
}

// TestRevokeWaiverWhitespaceNoBorrow 完整编号找不到时必须明确拒绝：
// 即使去掉空白后能碰上另一名学生或另一份免修，也绝不借用那份记录，
// 不改写编号、不合并申请、不新增免修历史。普通空格、制表符、全角空格
// 一样不能被忽略。
func TestRevokeWaiverWhitespaceNoBorrow(t *testing.T) {
	// 学生编号带各种前后空白且与既有编号都不完全一致：应报学生不存在，
	// 两名学生的 w1 都必须保持有效，且不产生任何变更。
	for _, student := range []string{" s1", "s1  ", "s1 ", "\ts1", "　s1"} {
		// s0 含 "s1" 与 " s1 "：这些输入都不等于其中任何一个。
		s, _ := mustLoadWhitespace(t, revokeWhitespaceTwoStudents())
		_, _, err := s.RevokeWaiver(student, "w1", "原因")
		if err == nil || !strings.Contains(err.Error(), "不存在") || !strings.Contains(err.Error(), "学生") {
			t.Fatalf("学生 %q 不存在，撤销应明确报学生不存在，得到 %v", student, err)
		}
		for _, sid := range []string{"s1", " s1 "} {
			if got := s.Waiver(sid, "w1"); got == nil || got.Status != WaiverApproved {
				t.Fatalf("学生 %q 被拒绝后 %s 的 w1 不应变化，得到 %+v", student, sid, got)
			}
		}
		if s.Dirty() {
			t.Fatalf("被拒绝的撤销不应产生变更（学生 %q）", student)
		}
	}

	// 学生存在但免修编号带各种前后空白：应报该学生名下不存在该免修，
	// 既有的 w1 保持有效，不产生变更。
	for _, waiverID := range []string{" w1", "w1  ", "w1 ", "\tw1", "　w1"} {
		s, _ := mustLoadWhitespace(t, revokeWhitespaceTwoStudents())
		_, _, err := s.RevokeWaiver("s1", waiverID, "原因")
		if err == nil || !strings.Contains(err.Error(), "不存在") {
			t.Fatalf("免修 %q 不存在于 s1 名下，撤销应明确拒绝，得到 %v", waiverID, err)
		}
		if !strings.Contains(err.Error(), "s1") {
			t.Fatalf("错误应点名学生 s1 名下不存在该免修，得到 %v", err)
		}
		if got := s.Waiver("s1", "w1"); got == nil || got.Status != WaiverApproved {
			t.Fatalf("免修 %q 被拒绝后 s1 的 w1 不应变化，得到 %+v", waiverID, got)
		}
		if n := len(s.Waivers("s1")); n != 1 {
			t.Fatalf("被拒绝的撤销不应新增免修历史，s1 应有 1 条，得到 %d", n)
		}
		if s.Dirty() {
			t.Fatalf("被拒绝的撤销不应产生变更（免修 %q）", waiverID)
		}
	}
}

// TestRevokeWaiverWhitespaceRejectedStillRejected 完整编号命中的若是一份
// 已拒绝申请，仍不能撤销，也不能借用另一名学生同号的有效免修；另一名
// 学生的有效免修保持原样。
func TestRevokeWaiverWhitespaceRejectedStillRejected(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
		},
		Students: []*Student{{ID: "s1"}, {ID: " s1 "}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: "c1"},
		},
		Waivers: []*Waiver{
			// s1 的 w1 有效。
			{ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "竞赛获奖", Status: WaiverApproved},
			// " s1 " 的 w1 是当时目标要求不存在而被拒绝的申请。
			{ID: "w1", StudentID: " s1 ", ReqID: "rX", Basis: "材料",
				Status: WaiverRejected, Reason: "目标要求 rX 不存在或不属于该学生"},
		},
	}
	s, _ := mustLoadWhitespace(t, d)

	_, _, err := s.RevokeWaiver(" s1 ", "w1", "撤销原因")
	if err == nil || !strings.Contains(err.Error(), "已被拒绝") {
		t.Fatalf("撤销 \" s1 \" 名下已拒绝的 w1 应被拒绝，得到 %v", err)
	}
	// 命中的是本人的拒绝记录：拒绝原因保留，状态不变。
	if got := s.Waiver(" s1 ", "w1"); got == nil || got.Status != WaiverRejected {
		t.Fatalf("\" s1 \" 的已拒绝 w1 应保持原样，得到 %+v", got)
	}
	// 不能借用 s1 的同号有效免修：s1 的 w1 仍有效、学分不变。
	if got := s.Waiver("s1", "w1"); got == nil || got.Status != WaiverApproved {
		t.Fatalf("s1 的有效 w1 不应受影响，得到 %+v", got)
	}
	if rep := s.CheckStudent("s1"); rep.TotalCredits != 4 {
		t.Fatalf("s1 应仍获得 4 学分，得到 %d", rep.TotalCredits)
	}
	if s.Dirty() {
		t.Fatal("被拒绝的撤销不应产生变更")
	}
}

// TestRevokeWaiverWhitespaceRulesAfterExactHit 准确命中带空白的编号后
// 沿用现有规则：有通过记录时撤销后仍只计一份课程学分、来源恢复为最先
// 提交的通过记录；重复撤销返回原结果且后来的原因不覆盖首次撤销原因。
func TestRevokeWaiverWhitespaceRulesAfterExactHit(t *testing.T) {
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
			{ID: "e2", StudentID: " s1 ", ReqID: "r1", Term: "2024春", Result: Passed, ResultSeq: 1},
			{ID: "e1", StudentID: " s1 ", ReqID: "r1", Term: "2024秋", Result: Passed, ResultSeq: 2},
		},
		Waivers: []*Waiver{
			{ID: "w1", StudentID: " s1 ", ReqID: "r1", Basis: "竞赛获奖", Status: WaiverApproved},
		},
		NextResultSeq: 2,
	}
	s, _ := mustLoadWhitespace(t, d)

	w, changed, err := s.RevokeWaiver(" s1 ", "w1", "首次撤销：材料存疑")
	if err != nil || !changed {
		t.Fatalf("首次撤销应成功，changed=%v err=%v", changed, err)
	}
	// 有通过记录：继续满足、只计一份 4 学分，来源恢复为最先提交的 e2。
	rep := s.CheckStudent(" s1 ")
	if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
		t.Fatalf("撤销后有通过记录应继续满足、仍为 4 学分，得到 %+v", rep)
	}
	st := rep.Requirements[0]
	if st.Source != "enrollment" || st.PassedEnrollmentID != "e2" {
		t.Fatalf("撤销后来源应恢复为最先提交通过的 e2，得到 %+v", st)
	}
	if len(st.PassedEnrollmentIDs) != 2 {
		t.Fatalf("修读历史应保留两份通过记录，得到 %v", st.PassedEnrollmentIDs)
	}

	// 重复撤销：返回原结果，后来给出的原因不覆盖首次撤销原因，不再产生变更。
	again, changed, err := s.RevokeWaiver(" s1 ", "w1", "后来给出的另一原因")
	if err != nil || changed || again != w {
		t.Fatalf("重复撤销应幂等返回原记录，changed=%v err=%v again=%+v", changed, err, again)
	}
	if w.Reason != "首次撤销：材料存疑" || w.Status != WaiverRevoked {
		t.Fatalf("首次撤销原因不应被覆盖，得到 %+v", w)
	}
	if n := len(s.Waivers(" s1 ")); n != 1 {
		t.Fatalf("重复撤销不应新增免修历史，得到 %d 条", n)
	}
}
