package credit

import (
	"os"
	"strings"
	"testing"
)

// 本文件回归“打开已有记录文件时，已撤销免修也必须保留一份曾经有效的申请
// 所必需的信息”。已撤销只取消免修对课程要求的满足作用，不能让缺少原依据、
// 失去要求归属的记录成为合法历史。这类残缺记录无法经正常申请-撤销流程产生
// （撤销只作用于当时有效的免修），所以用例直接构造记录文件，聚焦 Load：
//   - 已撤销免修的目标要求必须真实存在于该学生名下：要求编号为空、要求根本
//     不存在、同号要求只属于另一名学生，都按文件内容损坏拒绝；
//   - 原依据必须含非空白内容：缺失、空串、全为空格/制表符/换行都拒绝；
//     依据含实际文字时两侧及内部原有空白原样保留，检查不改写材料；
//   - 即使该要求另有足以满足它的通过修读、即使查询的是另一名学生，也不能
//     跳过问题记录；原文件逐字节保留；
//   - 合法的已撤销历史正常读取：原编号、依据、撤销原因与排列顺序保持原样，
//     不重新产生学分，不纳入有效免修唯一性限制（同一要求可保留多份已撤销，
//     还可同时有一份有效）；
//   - 被拒绝的申请允许因要求不存在或依据为空而留下记录，不沿用已撤销的
//     检查条件。

// revokedBase 构造结构完整的基础记录：s1 的要求 r1 指向 4 学分课程 c1。
func revokedBase() *fileData {
	return &fileData{
		Version:      recordVersion,
		Courses:      []*Course{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
	}
}

// assertRevokedCorrupt 是残缺已撤销记录用例的共同断言：Load 必须失败、
// 标记文件已存在，错误点名记录文件、所属学生、免修编号，并说明是目标要求
// 无效还是原依据为空；原文件字节完整保留。
func assertRevokedCorrupt(t *testing.T, path string, raw []byte, student, waiver, kind string) {
	t.Helper()
	s, existed, err := Load(path)
	if err == nil {
		t.Fatalf("残缺的已撤销免修必须按内容损坏整份拒绝，却得到 store=%v", s)
	}
	if !existed {
		t.Fatalf("残缺记录应标记为文件已存在，existed=%v err=%v", existed, err)
	}
	msg := err.Error()
	for _, want := range []string{path, "内容损坏", student, waiver, kind} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误信息应包含 %q（文件/学生 %s/免修 %s/原因 %s），得到：%v",
				want, student, waiver, kind, err)
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

// TestLoadRevokedWaiverRequiresOwnedRequirement 已撤销免修的目标要求必须真实
// 存在于其所属学生名下：编号为空、要求不存在、同号要求只属于另一名学生，
// 一律按损坏拒绝，错误说明“目标要求无效”。
func TestLoadRevokedWaiverRequiresOwnedRequirement(t *testing.T) {
	cases := map[string]func(d *fileData) *Waiver{
		"要求编号为空": func(d *fileData) *Waiver {
			return &Waiver{ID: "wv", StudentID: "s1", ReqID: "",
				Basis: "原竞赛材料", Status: WaiverRevoked, Reason: "材料无法核实"}
		},
		"要求根本不存在": func(d *fileData) *Waiver {
			return &Waiver{ID: "wv", StudentID: "s1", ReqID: "rX",
				Basis: "原竞赛材料", Status: WaiverRevoked, Reason: "材料无法核实"}
		},
		"同号要求仅属于另一名学生": func(d *fileData) *Waiver {
			// s2 名下确有 r1，但不能借给 s1 的已撤销历史：基础记录里 s1 自己
			// 的 r1 必须去掉，使 r1 只存在于 s2 名下。
			d.Requirements = nil
			d.Courses = append(d.Courses, &Course{ID: "c2", Name: "线性代数", Credit: 3, Open: true})
			d.Students = append(d.Students, &Student{ID: "s2"})
			d.Requirements = append(d.Requirements,
				&Requirement{ID: "r1", StudentID: "s2", CourseID: "c2"})
			return &Waiver{ID: "wv", StudentID: "s1", ReqID: "r1",
				Basis: "原竞赛材料", Status: WaiverRevoked, Reason: "材料无法核实"}
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			d := revokedBase()
			w := build(d)
			d.Waivers = []*Waiver{w}
			path, raw := writeRecord(t, d)
			assertRevokedCorrupt(t, path, raw, "s1", "wv", "目标要求无效")
		})
	}
}

// TestLoadRevokedWaiverRequiresNonBlankBasis 已撤销免修的原依据必须含非空白
// 内容：空字符串与全由空格、制表符、换行组成的依据都按损坏拒绝，错误说明
// “原依据为空”。
func TestLoadRevokedWaiverRequiresNonBlankBasis(t *testing.T) {
	for _, basis := range []string{"", "   ", "\t\t", "\n", " \t\r\n  "} {
		d := revokedBase()
		d.Waivers = []*Waiver{{
			ID: "wv", StudentID: "s1", ReqID: "r1", Basis: basis,
			Status: WaiverRevoked, Reason: "材料无法核实",
		}}
		path, raw := writeRecord(t, d)
		assertRevokedCorrupt(t, path, raw, "s1", "wv", "原依据为空")
	}
}

// TestLoadRevokedWaiverCorruptionNotMasked 即使残缺已撤销记录所在的要求另有
// 通过修读（本来足以满足要求），整份文件仍须拒绝；即使记录属于 s1、本次只
// 查询完全无关的另一名学生 s2，也不能跳过问题记录继续核对。
func TestLoadRevokedWaiverCorruptionNotMasked(t *testing.T) {
	t.Run("通过修读不能掩盖残缺已撤销记录", func(t *testing.T) {
		d := revokedBase()
		d.Enrollments = []*Enrollment{{
			ID: "e1", StudentID: "s1", ReqID: "r1", Term: "2024春",
			Result: Passed, ResultSeq: 1,
		}}
		d.NextResultSeq = 1
		d.Waivers = []*Waiver{{
			ID: "wv", StudentID: "s1", ReqID: "rX", Basis: "原竞赛材料",
			Status: WaiverRevoked, Reason: "材料无法核实",
		}}
		path, raw := writeRecord(t, d)
		assertRevokedCorrupt(t, path, raw, "s1", "wv", "目标要求无效")

		// Load 失败即整份不可用：不能拿着通过修读输出部分核对结果。
		if s, _, err := Load(path); err == nil {
			t.Fatalf("不能跳过问题记录继续核对，得到 store=%v", s)
		}
	})

	t.Run("查询另一名学生也不能跳过问题记录", func(t *testing.T) {
		d := revokedBase()
		// s2 自身的课程、要求、通过修读完全合法。
		d.Courses = append(d.Courses, &Course{ID: "c2", Name: "线性代数", Credit: 3, Open: true})
		d.Students = append(d.Students, &Student{ID: "s2"})
		d.Requirements = append(d.Requirements,
			&Requirement{ID: "r9", StudentID: "s2", CourseID: "c2"})
		d.Enrollments = []*Enrollment{{
			ID: "e9", StudentID: "s2", ReqID: "r9", Term: "2024春",
			Result: Passed, ResultSeq: 1,
		}}
		d.NextResultSeq = 1
		d.Waivers = []*Waiver{{
			ID: "wv", StudentID: "s1", ReqID: "rX", Basis: "原竞赛材料",
			Status: WaiverRevoked, Reason: "材料无法核实",
		}}
		path, raw := writeRecord(t, d)
		s, existed, err := Load(path)
		if err == nil {
			t.Fatalf("s1 的残缺历史应让整份文件不可读，连 s2 都查不了，store=%v", s)
		}
		if !existed {
			t.Fatalf("应标记文件已存在，existed=%v", existed)
		}
		msg := err.Error()
		if !strings.Contains(msg, "s1") || !strings.Contains(msg, "wv") {
			t.Fatalf("错误应归属问题记录的学生 s1 与免修 wv，得到：%v", err)
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

// TestLoadLegitRevokedHistoryPreserved 合法的已撤销历史正常读取：同一要求可
// 保留多份已撤销免修并同时有一份有效免修；失效历史不纳入有效免修唯一性
// 限制、不重新产生学分；原编号、依据、撤销原因与排列顺序保持原样。
func TestLoadLegitRevokedHistoryPreserved(t *testing.T) {
	d := revokedBase()
	d.Waivers = []*Waiver{
		{ID: "wv1", StudentID: "s1", ReqID: "r1", Basis: "第一次竞赛材料",
			Status: WaiverRevoked, Reason: "材料无法核实"},
		{ID: "wa", StudentID: "s1", ReqID: "r1", Basis: "外校同层次课程",
			Status: WaiverApproved},
		{ID: "wv2", StudentID: "s1", ReqID: "r1", Basis: "第二次竞赛材料",
			Status: WaiverRevoked, Reason: "重复撤销不改变结果"},
	}
	path, _ := writeRecord(t, d)
	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("多份已撤销与一份有效免修共存应正常打开，existed=%v err=%v", existed, err)
	}

	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 4 {
		t.Fatalf("只应由有效免修 wa 计一次 4 学分，已撤销历史不重新产生学分，得到 %d",
			rep.TotalCredits)
	}
	if len(rep.Unmet) != 0 {
		t.Fatalf("r1 应由有效免修满足，未满足=%v", rep.Unmet)
	}
	if st := rep.Requirements[0]; !st.Satisfied || st.Source != "waiver" || st.WaiverID != "wa" {
		t.Fatalf("来源应是有效免修 wa，得到 %+v", st)
	}
	if len(rep.RevokedWaivers) != 2 || rep.RevokedWaivers[0] != "wv1" ||
		rep.RevokedWaivers[1] != "wv2" {
		t.Fatalf("应按顺序列出两份已撤销免修 wv1、wv2，得到 %v", rep.RevokedWaivers)
	}

	// 原编号、依据、撤销原因与排列顺序逐条保持。
	ws := s.Waivers("s1")
	want := []struct {
		id, basis, reason string
		st                WaiverStatus
	}{
		{"wv1", "第一次竞赛材料", "材料无法核实", WaiverRevoked},
		{"wa", "外校同层次课程", "", WaiverApproved},
		{"wv2", "第二次竞赛材料", "重复撤销不改变结果", WaiverRevoked},
	}
	if len(ws) != 3 {
		t.Fatalf("免修历史应完整保留 3 条，得到 %d", len(ws))
	}
	for i, w := range want {
		got := ws[i]
		if got.ID != w.id || got.Basis != w.basis || got.Status != w.st ||
			got.Reason != w.reason {
			t.Fatalf("第 %d 条历史应原样保留，想要 id=%s basis=%q status=%s reason=%q，得到 %+v",
				i, w.id, w.basis, w.st, w.reason, got)
		}
	}
}

// TestLoadRevokedWaiverKeepsBasisWhitespace 依据含实际文字时，其两侧及内部
// 原有的空格、制表符、换行必须原样保留——检查只读内容，绝不为判空而改写
// 保存下来的材料；重新保存再加载也不丢空白。
func TestLoadRevokedWaiverKeepsBasisWhitespace(t *testing.T) {
	basis := "  学科竞赛\t获奖\n材料  "
	d := revokedBase()
	d.Waivers = []*Waiver{{
		ID: "wv", StudentID: "s1", ReqID: "r1", Basis: basis,
		Status: WaiverRevoked, Reason: "材料无法核实",
	}}
	path, _ := writeRecord(t, d)
	s, _, err := Load(path)
	if err != nil {
		t.Fatalf("含实际文字与空白的依据应正常加载：%v", err)
	}
	w := s.Waiver("s1", "wv")
	if w == nil {
		t.Fatal("已撤销免修 wv 应在历史中")
	}
	if w.Basis != basis {
		t.Fatalf("依据中的原有空白必须原样保留，\nwant=%q\n got=%q", basis, w.Basis)
	}

	// 重新保存后再次加载：空白与撤销状态仍完全保持。
	path2 := path + ".resaved"
	if err := s.Save(path2); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, _, err := Load(path2)
	if err != nil {
		t.Fatalf("重新保存后应可再次加载：%v", err)
	}
	if w2 := reloaded.Waiver("s1", "wv"); w2 == nil || w2.Basis != basis ||
		w2.Status != WaiverRevoked || w2.Reason != "材料无法核实" {
		t.Fatalf("重新保存后依据空白与撤销历史应保持，得到 %+v", w2)
	}
}

// TestLoadRevokedWaiverCheckedPerStudent 不同学生各自拥有同号要求与同号已
// 撤销免修是合法情况：不能把他人的要求借给当前学生（s1 没有本人的 r1 时
// 仍要拒绝），也不能把各自独立的历史误判成重复。
func TestLoadRevokedWaiverCheckedPerStudent(t *testing.T) {
	t.Run("各自有本人要求与同号已撤销免修_合法", func(t *testing.T) {
		d := &fileData{
			Version: recordVersion,
			Courses: []*Course{
				{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
				{ID: "c2", Name: "线性代数", Credit: 3, Open: true},
			},
			Students: []*Student{{ID: "s1"}, {ID: "s2"}},
			Requirements: []*Requirement{
				{ID: "r1", StudentID: "s1", CourseID: "c1"},
				{ID: "r1", StudentID: "s2", CourseID: "c2"},
			},
			Waivers: []*Waiver{
				{ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "s1 的依据",
					Status: WaiverRevoked, Reason: "s1 撤销"},
				{ID: "w1", StudentID: "s2", ReqID: "r1", Basis: "s2 的依据",
					Status: WaiverRevoked, Reason: "s2 撤销"},
			},
		}
		path, _ := writeRecord(t, d)
		s, _, err := Load(path)
		if err != nil {
			t.Fatalf("两名学生各自的同号已撤销历史应正常读取：%v", err)
		}
		w1 := s.Waiver("s1", "w1")
		w2 := s.Waiver("s2", "w1")
		if w1 == nil || w2 == nil || w1 == w2 {
			t.Fatalf("两人的同号已撤销免修应是各自独立的记录，得到 %p %p", w1, w2)
		}
		if w1.Basis != "s1 的依据" || w1.Reason != "s1 撤销" ||
			w2.Basis != "s2 的依据" || w2.Reason != "s2 撤销" {
			t.Fatalf("两份历史的依据与撤销原因应各归本人，得到 %+v %+v", w1, w2)
		}
		// 都已撤销：两人的要求均未满足、学分为零。
		for _, st := range []string{"s1", "s2"} {
			if rep := s.CheckStudent(st); rep.TotalCredits != 0 || len(rep.Unmet) != 1 {
				t.Fatalf("%s 的已撤销历史不应产生学分，得到 %+v", st, rep)
			}
		}
	})

	t.Run("s2有r1但s1没有_s1的已撤销记录仍拒绝", func(t *testing.T) {
		d := revokedBase()
		// 基础记录里 s1 有 r1；删掉它，只给 s2 一个 r1，s1 的已撤销 w1 立即
		// 失去要求归属。
		d.Requirements = nil
		d.Courses = append(d.Courses, &Course{ID: "c2", Name: "线性代数", Credit: 3, Open: true})
		d.Students = append(d.Students, &Student{ID: "s2"})
		d.Requirements = append(d.Requirements,
			&Requirement{ID: "r1", StudentID: "s2", CourseID: "c2"})
		d.Waivers = []*Waiver{{
			ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "原竞赛材料",
			Status: WaiverRevoked, Reason: "材料无法核实",
		}}
		path, raw := writeRecord(t, d)
		assertRevokedCorrupt(t, path, raw, "s1", "w1", "目标要求无效")
	})
}

// TestLoadRejectedWaiversExemptFromRevokedChecks 被拒绝的申请允许因要求不
// 存在（含要求只属于他人或编号缺失的业务等价情形——这里只覆盖正常流程能
// 留下的“要求不存在”）或依据为空（空串、纯空白）而留下记录与原拒绝原因：
// 不能沿用已撤销免修的检查条件把它们误报成文件损坏。
func TestLoadRejectedWaiversExemptFromRevokedChecks(t *testing.T) {
	d := revokedBase()
	d.Courses = append(d.Courses,
		&Course{ID: "c2", Name: "线性代数", Credit: 3, Open: true},
		&Course{ID: "c3", Name: "大学英语", Credit: 2, Open: true})
	d.Students = append(d.Students, &Student{ID: "s2"})
	d.Requirements = append(d.Requirements,
		&Requirement{ID: "r1", StudentID: "s2", CourseID: "c2"},
		&Requirement{ID: "r2", StudentID: "s2", CourseID: "c3"})
	d.Waivers = []*Waiver{
		// 要求在 s1 名下根本不存在。
		{ID: "wj1", StudentID: "s1", ReqID: "rX", Basis: "找不到要求的依据",
			Status: WaiverRejected, Reason: "目标要求 rX 不存在或不属于该学生"},
		// 依据为空字符串。
		{ID: "wj2", StudentID: "s1", ReqID: "r1", Basis: "",
			Status: WaiverRejected, Reason: "免修依据为空"},
		// 依据全为空白。
		{ID: "wj3", StudentID: "s1", ReqID: "r1", Basis: " \t\n ",
			Status: WaiverRejected, Reason: "免修依据为空"},
		// 同号要求 r2 只属于另一名学生 s2。
		{ID: "wj4", StudentID: "s1", ReqID: "r2", Basis: "借名申请的依据",
			Status: WaiverRejected, Reason: "目标要求 r2 不存在或不属于该学生"},
	}
	path, raw := writeRecord(t, d)
	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("被拒绝的残缺申请是正常历史，应正常打开，existed=%v err=%v", existed, err)
	}
	rep := s.CheckStudent("s1")
	if len(rep.RejectedWaivers) != 4 {
		t.Fatalf("应保留 4 条被拒绝历史，得到 %d：%+v", len(rep.RejectedWaivers), rep.RejectedWaivers)
	}
	for i, want := range []struct {
		id, reason string
	}{
		{"wj1", "目标要求 rX 不存在或不属于该学生"},
		{"wj2", "免修依据为空"},
		{"wj3", "免修依据为空"},
		{"wj4", "目标要求 r2 不存在或不属于该学生"},
	} {
		rj := rep.RejectedWaivers[i]
		if rj.Waiver.ID != want.id || rj.Reason != want.reason {
			t.Fatalf("第 %d 条拒绝历史应保留原编号与原拒绝原因，想要 %s/%q，得到 %+v",
				i, want.id, want.reason, rj)
		}
	}
	// 这些记录都是 rejected：s1 的 r1 仍未满足，不产生学分，也没有已撤销列表。
	if rep.TotalCredits != 0 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("被拒绝历史不满足要求、不产生学分，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	if len(rep.RevokedWaivers) != 0 {
		t.Fatalf("不应有误报的已撤销免修，得到 %v", rep.RevokedWaivers)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(raw) {
		t.Fatal("只读核对不得改写合法历史文件")
	}
}
