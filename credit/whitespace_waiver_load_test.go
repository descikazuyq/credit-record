package credit

import (
	"strings"
	"testing"
)

// 本文件为“申请免修时按用户给出的完整学生编号、要求编号与免修编号确定
// 申请归属”这项行为提供回归保障。记录文件可以合法保存带前后空白（普通
// 空格、制表符、全角空格 U+3000）的编号，成绩提交与免修撤销已按完整
// 编号办理，申请免修也必须如此：
//   - “s1”与“ s1 ”是两名不同学生，同一学生名下“r1”与“ r1 ”是两项
//     不同要求，“w1”与“ w1 ”是两份不同申请；普通空格、制表符、全角
//     空格都是编号内容，绝不能为了匹配而删掉；
//   - 给带空白的编号提交申请只命中编号完全一致的学生与要求，提示、免修
//     历史与核对来源都保留申请实际使用的编号，另一名学生的学分、要求与
//     申请保持原样，不改写已有编号、不合并记录；
//   - 完整学生编号不存在时按业务规则直接报错，不在任何学生名下新增历史；
//     学生存在而完整要求编号不存在时，即使去掉空白能碰上另一项要求，也
//     绝不借用，而是按原规则在申请人名下保存原要求编号、免修编号、依据
//     原文与拒绝原因，不获得学分；
//   - 编号为空字符串时直接拒绝，不创建申请；
//   - 命中已有申请后沿用现有规则：内容一致返回原结果，内容冲突拒绝修改，
//     已拒绝/已撤销的申请不因重试重新生效；新编号申请一项已有有效免修的
//     要求，仍只保留一条被拒绝的新申请，不产生第二份有效免修；
//   - 依据含实际文字时其前后与中间空白原样保留，纯空白依据仍按业务拒绝。

// waiverWhitespaceTwoStudents 构造两名学生 "s1" 与 " s1 "：各有一项编号
// 为 r1 的要求，分别指向 4 学分课程 c1 与 3 学分课程 c2，两人都没有通过
// 修读或有效免修。
func waiverWhitespaceTwoStudents() *fileData {
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

// TestApplyWaiverWhitespaceStudentIDHitsExactOwner 给 " s1 " 提交含实际
// 文字依据的新申请：只能满足 " s1 " 本人的 r1、获得本人 3 学分课程的
// 学分；申请按完整编号登记在本人名下，"s1" 的学分、要求与免修历史保持
// 原样。
func TestApplyWaiverWhitespaceStudentIDHitsExactOwner(t *testing.T) {
	s, _ := mustLoadWhitespace(t, waiverWhitespaceTwoStudents())

	basis := "  学科竞赛获奖证明  "
	w, action, err := s.ApplyWaiver(" s1 ", "r1", "w1", basis)
	if err != nil || action != ActionCreated {
		t.Fatalf("给 \" s1 \" 的申请应正常新建，得到 %+v action=%v err=%v", w, action, err)
	}
	// 申请实际使用的完整编号与依据原文都必须保留，绝不修剪。
	if w.StudentID != " s1 " || w.ID != "w1" || w.ReqID != "r1" || w.Basis != basis {
		t.Fatalf("申请应保留完整编号与依据原文，得到 %+v", w)
	}
	if w.Status != WaiverApproved {
		t.Fatalf("依据含实际文字且要求存在应判有效，得到 %+v", w)
	}

	// " s1 " 本人：3 学分、来源为本人的 w1。
	rep := s.CheckStudent(" s1 ")
	if !rep.Found || rep.TotalCredits != 3 || len(rep.Unmet) != 0 {
		t.Fatalf("\" s1 \" 应凭本人 r1 获得 3 学分，得到 %+v", rep)
	}
	st := rep.Requirements[0]
	if st.Req.ID != "r1" || st.Course.ID != "c2" || st.Course.Credit != 3 ||
		!st.Satisfied || st.Source != "waiver" || st.WaiverID != "w1" {
		t.Fatalf("\" s1 \" 的学分来源应是本人 3 学分课程与本人 w1，得到 %+v", st)
	}

	// "s1" 保持原样：0 学分、r1 未满足、名下没有任何申请。
	rep = s.CheckStudent("s1")
	if rep.TotalCredits != 0 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("\"s1\" 应仍为 0 学分且 r1 未满足，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	if st := rep.Requirements[0]; st.Satisfied || st.Source != "" {
		t.Fatalf("\"s1\" 的要求不应被他人的申请满足，得到 %+v", st)
	}
	if len(rep.RejectedWaivers) != 0 || len(rep.RevokedWaivers) != 0 {
		t.Fatalf("\"s1\" 名下不应出现任何免修申请，得到 拒绝=%v 撤销=%v",
			rep.RejectedWaivers, rep.RevokedWaivers)
	}
	if ws := s.Waivers("s1"); len(ws) != 0 {
		t.Fatalf("\"s1\" 的免修历史应保持为空，得到 %+v", ws)
	}
	if ws := s.Waivers(" s1 "); len(ws) != 1 || ws[0] != w {
		t.Fatalf("申请应只记入 \" s1 \" 名下，得到 %+v", ws)
	}
}

// TestApplyWaiverWhitespaceReqIDIndependent 同一学生名下 "r1" 与 " r1 "
// 是两项不同要求（指向不同学分课程）：新申请只替代完整编号对应的那一项，
// 另一项保持未满足，两项要求互不借用。
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
	s, _ := mustLoadWhitespace(t, d)

	w, action, err := s.ApplyWaiver("s1", " r1 ", "w1", "外校同层次课程")
	if err != nil || action != ActionCreated || w.Status != WaiverApproved {
		t.Fatalf("申请只应命中完整编号为 \" r1 \" 的要求，得到 %+v action=%v err=%v",
			w, action, err)
	}
	if w.ReqID != " r1 " {
		t.Fatalf("申请应保留完整要求编号 \" r1 \"，得到 %+v", w)
	}

	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 3 {
		t.Fatalf("只应计 \" r1 \" 对应课程的 3 学分，得到 %d", rep.TotalCredits)
	}
	statuses := map[string]RequirementStatus{}
	for _, st := range rep.Requirements {
		statuses[st.Req.ID] = st
	}
	if st := statuses[" r1 "]; !st.Satisfied || st.Source != "waiver" ||
		st.WaiverID != "w1" || st.Course.Credit != 3 {
		t.Fatalf("\" r1 \" 应由 w1 满足、计 3 学分，得到 %+v", st)
	}
	if st := statuses["r1"]; st.Satisfied || st.Source != "" || st.Course.Credit != 4 {
		t.Fatalf("\"r1\" 不应被同号异写的申请满足，得到 %+v", st)
	}
	if len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("未满足要求应仍为 [r1]，得到 %v", rep.Unmet)
	}

	// 再给不带空白的 r1 提交另一份申请：各自独立生效，不判编号冲突。
	w2, action, err := s.ApplyWaiver("s1", "r1", "w2", "学科竞赛获奖")
	if err != nil || action != ActionCreated || w2.Status != WaiverApproved {
		t.Fatalf("给 r1 的新申请应独立生效，得到 %+v action=%v err=%v", w2, action, err)
	}
	if rep = s.CheckStudent("s1"); rep.TotalCredits != 7 || len(rep.Unmet) != 0 {
		t.Fatalf("两项要求各计一份学分应为 7，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
}

// TestApplyWaiverWhitespaceWaiverIDIndependent 同一学生名下 "w1" 与
// " w1 " 是两份不同申请：分别保留、各自幂等，不能因去空白而返回另一份
// 申请或按另一份申请的内容报冲突。
func TestApplyWaiverWhitespaceWaiverIDIndependent(t *testing.T) {
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
			{ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "依据一", Status: WaiverApproved},
			{ID: " w1 ", StudentID: "s1", ReqID: "r2", Basis: "依据二", Status: WaiverApproved},
		},
	}
	s, _ := mustLoadWhitespace(t, d)

	// 用 " w1 " 的原内容重复提交：必须返回带空格的那一份，不能返回 "w1"。
	got, action, err := s.ApplyWaiver("s1", "r2", " w1 ", "依据二")
	if err != nil || action != ActionExisted {
		t.Fatalf("\" w1 \" 原内容重复提交应幂等命中本人，得到 %+v action=%v err=%v",
			got, action, err)
	}
	if got.ID != " w1 " || got.ReqID != "r2" || got.Basis != "依据二" {
		t.Fatalf("幂等返回的应是 \" w1 \" 原申请，得到 %+v", got)
	}

	// 用 " w1 " 提交不同内容：冲突应针对 " w1 " 的原内容，绝不能拿 "w1"
	// 的内容当作既有申请报冲突。
	_, _, err = s.ApplyWaiver("s1", "r2", " w1 ", "换成别的依据")
	if err == nil {
		t.Fatal("同编号换内容应被拒绝")
	}
	msg := err.Error()
	if !strings.Contains(msg, " w1 ") || !strings.Contains(msg, "依据二") {
		t.Fatalf("冲突拒绝应点名 \" w1 \" 及其原依据，得到：%v", err)
	}
	if strings.Contains(msg, "依据一") {
		t.Fatalf("冲突说明不能引用另一份申请 \"w1\" 的依据，得到：%v", err)
	}

	// 两份申请各自原样保留。
	if w := s.Waiver("s1", "w1"); w == nil || w.ReqID != "r1" ||
		w.Basis != "依据一" || w.Status != WaiverApproved {
		t.Fatalf("\"w1\" 应保持原样，得到 %+v", w)
	}
	if w := s.Waiver("s1", " w1 "); w == nil || w.ReqID != "r2" ||
		w.Basis != "依据二" || w.Status != WaiverApproved {
		t.Fatalf("\" w1 \" 应保持原样，得到 %+v", w)
	}
	if ws := s.Waivers("s1"); len(ws) != 2 {
		t.Fatalf("两份申请应分别保留，得到 %+v", ws)
	}
}

// TestApplyWaiverWhitespaceNewIDDoesNotBorrowExisting 名下只有 "w1" 时，
// 用 " w1 " 提交（哪怕内容不同）是新编号的新申请，不能返回 "w1" 或按
// "w1" 的内容报冲突：目标要求已有有效免修时，按原规则保留一条被拒绝的
// 新申请，不产生第二份有效免修。
func TestApplyWaiverWhitespaceNewIDDoesNotBorrowExisting(t *testing.T) {
	s, _ := mustLoadWhitespace(t, waiverWhitespaceTwoStudents())

	first, action, err := s.ApplyWaiver(" s1 ", "r1", "w1", "依据一")
	if err != nil || action != ActionCreated || first.Status != WaiverApproved {
		t.Fatalf("首次申请应生效，得到 %+v action=%v err=%v", first, action, err)
	}

	// 用带空白的新编号再次申请同一要求：新建一条被拒绝记录，而不是硬冲突。
	second, action, err := s.ApplyWaiver(" s1 ", "r1", " w1 ", "另一份依据")
	if err != nil || action != ActionCreated {
		t.Fatalf("新编号申请应作为独立申请落为被拒绝记录，得到 %+v action=%v err=%v",
			second, action, err)
	}
	if second.ID != " w1 " || second.Status != WaiverRejected ||
		second.ReqID != "r1" || second.Basis != "另一份依据" {
		t.Fatalf("应保留新申请的完整编号、要求与依据，得到 %+v", second)
	}
	if !strings.Contains(second.Reason, "w1") {
		t.Fatalf("拒绝原因应点名已有有效免修 w1，得到 %+v", second)
	}

	// 原有效免修仍是唯一学分来源，3 学分只计一次。
	rep := s.CheckStudent(" s1 ")
	if rep.TotalCredits != 3 || len(rep.Unmet) != 0 {
		t.Fatalf("拒绝第二份申请后仍只计一次 3 学分，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	if st := rep.Requirements[0]; st.Source != "waiver" || st.WaiverID != "w1" {
		t.Fatalf("来源应仍是原有效免修 w1，得到 %+v", st)
	}
	if len(rep.RejectedWaivers) != 1 || rep.RejectedWaivers[0].Waiver != second {
		t.Fatalf("核对应列出被拒绝的新申请，得到 %+v", rep.RejectedWaivers)
	}

	// 原样重复提交被拒绝的新申请：返回原拒绝结果，不重新生效。
	again, action, err := s.ApplyWaiver(" s1 ", "r1", " w1 ", "另一份依据")
	if err != nil || action != ActionExisted || again != second ||
		again.Status != WaiverRejected {
		t.Fatalf("被拒绝申请重复提交应幂等返回原结果，得到 %+v action=%v err=%v",
			again, action, err)
	}
	if ws := s.Waivers(" s1 "); len(ws) != 2 {
		t.Fatalf("重复提交不应新增申请，得到 %+v", ws)
	}
}

// TestApplyWaiverWhitespaceStudentNotFoundHardReject 完整学生编号不存在
// 时直接按业务规则报错：不创建申请、不在任何学生（含去掉空白能碰上的
// 另一名学生）名下新增历史、不产生变更。
func TestApplyWaiverWhitespaceStudentNotFoundHardReject(t *testing.T) {
	cases := []string{" s1", "s1 ", "\ts1\t", "　s1　", "  s1  "}
	for _, student := range cases {
		t.Run(student, func(t *testing.T) {
			s, _ := mustLoadWhitespace(t, waiverWhitespaceTwoStudents())

			w, _, err := s.ApplyWaiver(student, "r1", "w1", "学科竞赛获奖")
			if err == nil {
				t.Fatalf("学生 %q 不存在应直接报错，却得到申请 %+v", student, w)
			}
			if !strings.Contains(err.Error(), "不存在") || !strings.Contains(err.Error(), "学生") {
				t.Fatalf("错误应明确说明学生不存在，得到：%v", err)
			}
			for _, sid := range []string{"s1", " s1 "} {
				if ws := s.Waivers(sid); len(ws) != 0 {
					t.Fatalf("学生 %q 被拒后不应在 %s 名下新增历史，得到 %+v",
						student, sid, ws)
				}
				if rep := s.CheckStudent(sid); len(rep.RejectedWaivers) != 0 {
					t.Fatalf("学生 %q 被拒后 %s 的核对不应出现拒绝记录", student, sid)
				}
			}
			if s.Dirty() {
				t.Fatalf("学生不存在的拒绝不应产生变更（学生 %q）", student)
			}
		})
	}
}

// TestApplyWaiverWhitespaceReqNotFoundSavedUnderApplicant 学生存在而完整
// 要求编号不存在时，即使去掉空白能碰上该学生名下的另一项要求，也按原
// 规则拒绝该申请：在申请人名下保存原要求编号、免修编号、依据原文与
// 拒绝原因，不借用另一项要求，不获得学分。
func TestApplyWaiverWhitespaceReqNotFoundSavedUnderApplicant(t *testing.T) {
	d := &fileData{
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
	cases := []struct {
		student string
		req     string
	}{
		{"s1", " r1 "},
		{"s1", "\tr1\t"},
		{"s1", "　r1　"},
		{" s1 ", " r1 "},
		{" s1 ", "r2"},
	}
	for _, tc := range cases {
		t.Run(tc.student+"/"+tc.req, func(t *testing.T) {
			s, _ := mustLoadWhitespace(t, d)

			w, action, err := s.ApplyWaiver(tc.student, tc.req, "w9", "实际依据文字")
			if err != nil || action != ActionCreated {
				t.Fatalf("目标要求不存在应软拒绝并保留申请，得到 %+v action=%v err=%v",
					w, action, err)
			}
			if w.Status != WaiverRejected {
				t.Fatalf("申请应标记为已拒绝，得到 %+v", w)
			}
			// 申请人名下保存的必须是原编号与原依据，绝不修剪。
			if w.StudentID != tc.student || w.ReqID != tc.req ||
				w.ID != "w9" || w.Basis != "实际依据文字" {
				t.Fatalf("拒绝记录应保留完整原编号与依据，得到 %+v", w)
			}
			if w.Reason == "" || !strings.Contains(w.Reason, tc.req) {
				t.Fatalf("拒绝原因应点名原要求编号 %q，得到 %+v", tc.req, w)
			}

			// 申请人不获得学分；其名下真实存在的 r1 仍未满足。
			rep := s.CheckStudent(tc.student)
			if rep.TotalCredits != 0 {
				t.Fatalf("被拒绝的申请不获得学分，得到 %d", rep.TotalCredits)
			}
			if len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
				t.Fatalf("真实要求 r1 应仍未满足，得到 %v", rep.Unmet)
			}
			if len(rep.RejectedWaivers) != 1 || rep.RejectedWaivers[0].Waiver != w {
				t.Fatalf("申请人核对中应列出这条拒绝记录，得到 %+v", rep.RejectedWaivers)
			}
			// 另一名学生名下不得出现任何历史，其要求与学分保持原样。
			other := "s1"
			if tc.student == "s1" {
				other = " s1 "
			}
			if ws := s.Waivers(other); len(ws) != 0 {
				t.Fatalf("拒绝历史不应记到另一名学生 %s 名下，得到 %+v", other, ws)
			}
		})
	}
}

// TestApplyWaiverWhitespaceEmptyIDsRejected 学生、要求、免修编号为空字符串
// 时一律直接拒绝：不创建申请、不产生变更；纯空白字符串仍是合法的完整编号
// （文件中不存在该学生时按学生不存在处理），不能与空字符串混为一谈。
func TestApplyWaiverWhitespaceEmptyIDsRejected(t *testing.T) {
	// 从记录文件加载，使 store 初始不带未落盘变更，从而能验证空编号拒绝
	// 不把 store 标记为已变更。
	s, _ := mustLoadWhitespace(t, &fileData{
		Version:      recordVersion,
		Courses:      []*Course{{ID: "c1", Name: "数学", Credit: 4, Open: true}},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
	})

	for _, tc := range [][4]string{
		{"", "r1", "w1", "依据"},
		{"s1", "", "w1", "依据"},
		{"s1", "r1", "", "依据"},
	} {
		w, _, err := s.ApplyWaiver(tc[0], tc[1], tc[2], tc[3])
		if err == nil {
			t.Fatalf("空编号 %v 应直接报错，却得到 %+v", tc, w)
		}
	}
	if ws := s.Waivers("s1"); len(ws) != 0 {
		t.Fatalf("空编号拒绝不得创建任何申请，得到 %+v", ws)
	}
	if s.Dirty() {
		t.Fatal("空编号拒绝不应产生变更")
	}

	// 纯空白学生编号不等于空字符串：文件中没有这名学生，按学生不存在
	// 硬拒绝，同样不创建任何申请。
	if _, _, err := s.ApplyWaiver(" ", "r1", "w1", "依据"); err == nil ||
		!strings.Contains(err.Error(), "不存在") {
		t.Fatalf("纯空白学生编号应按完整编号查找并报学生不存在，得到 %v", err)
	}
	if ws := s.Waivers("s1"); len(ws) != 0 {
		t.Fatalf("纯空白编号被拒后不得在 s1 名下新增历史，得到 %+v", ws)
	}
}

// TestApplyWaiverWhitespaceBasisRulesUnchanged 依据的文字处理保持兼容：
// 纯空白依据仍按业务规则拒绝并保留“免修依据为空”的原因；含实际文字时
// 前后与中间空白随原文保留，再次提交必须逐字符一致才算内容相同。
func TestApplyWaiverWhitespaceBasisRulesUnchanged(t *testing.T) {
	// 带空白编号的学生可直接经登记入口建立，也可来自记录文件。
	s, _ := mustLoadWhitespace(t, &fileData{
		Version:      recordVersion,
		Courses:      []*Course{{ID: "c2", Name: "线性代数", Credit: 3, Open: true}},
		Students:     []*Student{{ID: " s1 "}},
		Requirements: []*Requirement{{ID: "r1", StudentID: " s1 ", CourseID: "c2"}},
	})

	// 纯空白依据（空格、制表符、全角空格混用）：软拒绝，原因不变。
	w, action, err := s.ApplyWaiver(" s1 ", "r1", "wb", " \t 　 \n ")
	if err != nil || action != ActionCreated || w.Status != WaiverRejected ||
		w.Reason != "免修依据为空" {
		t.Fatalf("纯空白依据应按原规则软拒绝，得到 %+v action=%v err=%v", w, action, err)
	}
	if rep := s.CheckStudent(" s1 "); rep.TotalCredits != 0 ||
		len(rep.RejectedWaivers) != 1 {
		t.Fatalf("纯空白依据申请不获得学分但应保留历史，得到 %+v", rep)
	}

	// 含实际文字：依据原文（含前后空白）逐字符保留并判有效。
	basis := "\t 学科竞赛获奖　 \n "
	w, action, err = s.ApplyWaiver(" s1 ", "r1", "w1", basis)
	if err != nil || action != ActionCreated || w.Status != WaiverApproved {
		t.Fatalf("含实际文字的依据应判有效，得到 %+v action=%v err=%v", w, action, err)
	}
	if w.Basis != basis {
		t.Fatalf("依据原文应逐字符保留，\nwant=%q\n got=%q", basis, w.Basis)
	}

	// 去掉前后空白再提交：依据内容不同，按冲突拒绝，原依据保持不变。
	if _, _, err := s.ApplyWaiver(" s1 ", "r1", "w1", "学科竞赛获奖"); err == nil {
		t.Fatal("修剪后的依据与原文不同，应按内容冲突拒绝")
	}
	if got := s.Waiver(" s1 ", "w1"); got.Basis != basis || got.Status != WaiverApproved {
		t.Fatalf("冲突拒绝后原依据与有效状态应保持不变，得到 %+v", got)
	}

	// 原文再次提交：幂等返回原申请。
	if got, a, err := s.ApplyWaiver(" s1 ", "r1", "w1", basis); err != nil ||
		a != ActionExisted || got != w {
		t.Fatalf("原文重复提交应幂等返回原申请，得到 %+v action=%v err=%v", got, a, err)
	}
}
