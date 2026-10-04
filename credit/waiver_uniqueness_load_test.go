package credit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件为“同一学生的同一要求只能有一份有效免修”在读取历史记录时的唯一性
// 规则提供回归保障。互相重复取代的两份有效免修无法通过正常申请产生（提交时
// 第二份就会被拒绝），只能来自记录文件中既有的免修历史，因此以下用例直接
// 构造完整、可解析的记录文件，覆盖 Load 阶段的校验与读取后的核对行为。

// marshalFileData 按 Save 相同的方式序列化磁盘记录，便于逐字节比对。
func marshalFileData(t *testing.T, d *fileData) []byte {
	t.Helper()
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		t.Fatalf("序列化测试记录失败：%v", err)
	}
	return append(b, '\n')
}

// writeFileData 将构造好的磁盘记录写入临时文件，返回文件路径与原始字节。
func writeFileData(t *testing.T, d *fileData) (string, []byte) {
	t.Helper()
	raw := marshalFileData(t, d)
	path := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("写入测试记录失败：%v", err)
	}
	return path, raw
}

// conflictBaseData 构造一份除免修外完全合法的记录：学生 s1、4 学分课程 c1、
// 要求 r1 指向 c1。withPass 为真时附带一次已通过修读 e1。
func conflictBaseData(withPass bool) *fileData {
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

// twoApprovedWaivers 返回同一学生、同一要求下两份编号不同、依据均非空的
// 有效免修；两份申请单独看都完全合法，冲突只来自“同时有效”。
func twoApprovedWaivers() []*Waiver {
	return []*Waiver{
		{ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "学科竞赛获奖", Status: WaiverApproved},
		{ID: "w2", StudentID: "s1", ReqID: "r1", Basis: "外校同层次课程", Status: WaiverApproved},
	}
}

// TestLoadRejectsTwoApprovedWaiversForSameRequirement 即使记录文件完整、
// 可解析，学生、课程、要求都存在且两份申请都带非空依据，只要同一学生的
// 一项要求下有两份编号不同、状态均为有效的免修，就必须整份拒绝：
// 不能挑其中一份继续核对，也不能把后一份自动改成已拒绝。错误必须点名
// 问题文件、所属学生、目标要求与冲突的两个免修编号；原文件原样保留。
func TestLoadRejectsTwoApprovedWaiversForSameRequirement(t *testing.T) {
	waivers := twoApprovedWaivers()
	cases := map[string][]*Waiver{
		"第二份有效免修排在后面": {waivers[0], waivers[1]},
		"第二份有效免修排在前面": {waivers[1], waivers[0]},
	}
	for name, ws := range cases {
		t.Run(name, func(t *testing.T) {
			d := conflictBaseData(false)
			d.Waivers = ws
			path, raw := writeFileData(t, d)

			s, existed, err := Load(path)
			if err == nil {
				t.Fatalf("两份有效免修重复取代同一要求时必须拒绝整份记录，得到 store=%v", s)
			}
			if s != nil {
				t.Fatalf("冲突记录必须整份拒绝、不得返回可用 store，得到 %v", s)
			}
			if !existed {
				t.Fatal("冲突文件应标记为已存在")
			}
			msg := err.Error()
			for _, want := range []string{
				path,       // 问题文件
				"内容损坏",     // 沿用内容损坏的报错口径（退出码 2）
				"s1",       // 所属学生
				"r1",       // 目标要求
				"w1", "w2", // 冲突的两个免修编号
				"未做任何修改", // 不得改写原文件
			} {
				if !strings.Contains(msg, want) {
					t.Fatalf("错误信息应包含 %q，得到：%v", want, err)
				}
			}

			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != string(raw) {
				t.Fatalf("拒绝读取不得改写原文件\nwant=%q\n got=%q", raw, got)
			}
			// 再次打开仍必须拒绝：不能因第一次访问把记录“修正”成可打开状态。
			if s2, _, err2 := Load(path); err2 == nil {
				t.Fatalf("冲突记录再次读取仍应拒绝，得到 store=%v", s2)
			}
		})
	}
}

// TestLoadWaiverConflictNotMaskedByPassedEnrollment 目标要求即使另有通过
// 修读（不看免修也能满足、能计学分），也不能掩盖两份有效免修的冲突：
// 读取仍按内容损坏拒绝，不能返回“以修读满足”的核对结果。
func TestLoadWaiverConflictNotMaskedByPassedEnrollment(t *testing.T) {
	d := conflictBaseData(true)
	d.Waivers = twoApprovedWaivers()
	path, raw := writeFileData(t, d)

	s, existed, err := Load(path)
	if err == nil {
		t.Fatalf("通过修读不能掩盖免修冲突，却得到 store=%v", s)
	}
	if !existed || s != nil {
		t.Fatalf("冲突记录应整份拒绝：existed=%v store=%v", existed, s)
	}
	msg := err.Error()
	for _, want := range []string{"内容损坏", "s1", "r1", "w1", "w2"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误信息应包含 %q，得到：%v", want, err)
		}
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != string(raw) {
		t.Fatalf("原文件必须完整保留，readErr=%v", readErr)
	}
}

// TestLoadOneApprovedWithRejectedAndRevokedHistory 同一学生的同一要求可以
// 同时保留一份有效免修、一份已拒绝申请和一份已撤销申请：这样的记录必须
// 正常打开。核对只以那份有效免修说明来源且课程学分只计一次；查看历史时
// 各份申请的编号、依据、状态及原有拒绝/撤销原因都对应原记录，既不能为了
// 去重丢掉历史，也不能让失效申请重新参与满足要求。失效历史排在有效申请
// 之前或之后，结论必须相同。
func TestLoadOneApprovedWithRejectedAndRevokedHistory(t *testing.T) {
	approved := &Waiver{
		ID: "wok", StudentID: "s1", ReqID: "r1",
		Basis: "学科竞赛获奖", Status: WaiverApproved,
	}
	// 空依据被拒的真实历史（依据为空、拒绝原因保留）。
	rejected := &Waiver{
		ID: "wrej", StudentID: "s1", ReqID: "r1",
		Basis: "", Status: WaiverRejected, Reason: "免修依据为空",
	}
	revoked := &Waiver{
		ID: "wrev", StudentID: "s1", ReqID: "r1",
		Basis: "已过期的外校证明", Status: WaiverRevoked, Reason: "材料无法核实",
	}

	cases := map[string][]*Waiver{
		"失效历史排在有效申请之前": {revoked, rejected, approved},
		"失效历史排在有效申请之后": {approved, rejected, revoked},
	}
	for name, ws := range cases {
		t.Run(name, func(t *testing.T) {
			d := conflictBaseData(false)
			d.Waivers = ws
			path, raw := writeFileData(t, d)

			s, existed, err := Load(path)
			if err != nil || !existed {
				t.Fatalf("一份有效 + 已拒绝 + 已撤销的记录应正常打开：existed=%v err=%v",
					existed, err)
			}

			// 核对：只以有效免修说明来源，4 学分只计一次。
			rep := s.CheckStudent("s1")
			if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
				t.Fatalf("应只凭有效免修计一次 4 学分且无未满足，得到学分=%d 未满足=%v",
					rep.TotalCredits, rep.Unmet)
			}
			if len(rep.Requirements) != 1 {
				t.Fatalf("应只有一项要求，得到 %d 项", len(rep.Requirements))
			}
			st := rep.Requirements[0]
			if !st.Satisfied || st.Source != "waiver" || st.WaiverID != "wok" {
				t.Fatalf("来源必须是唯一的有效免修 wok，得到 %+v", st)
			}
			if len(st.PassedEnrollmentIDs) != 0 {
				t.Fatalf("没有通过修读，通过历史应为空，得到 %v", st.PassedEnrollmentIDs)
			}
			// 失效申请不能在核对中“复活”：拒绝与撤销各只列一条原记录。
			if len(rep.RejectedWaivers) != 1 {
				t.Fatalf("应保留 1 条被拒绝申请，得到 %+v", rep.RejectedWaivers)
			}
			rj := rep.RejectedWaivers[0].Waiver
			if rj.ID != "wrej" || rj.ReqID != "r1" || rj.Basis != "" ||
				rj.Status != WaiverRejected ||
				rep.RejectedWaivers[0].Reason != "免修依据为空" {
				t.Fatalf("被拒绝记录的编号、依据、状态与原因应对应原记录，得到 %+v",
					rep.RejectedWaivers[0])
			}
			if len(rep.RevokedWaivers) != 1 || rep.RevokedWaivers[0] != "wrev" {
				t.Fatalf("应列出 1 条已撤销免修 wrev，得到 %v", rep.RevokedWaivers)
			}

			// 免修历史：三份都在，编号、依据、状态、原因与原记录一致，顺序保持。
			got := s.Waivers("s1")
			if len(got) != 3 {
				t.Fatalf("不能为去重丢掉任何历史，应有 3 条，得到 %d", len(got))
			}
			for i, want := range ws {
				if got[i].ID != want.ID || got[i].Status != want.Status ||
					got[i].Basis != want.Basis || got[i].Reason != want.Reason ||
					got[i].ReqID != "r1" {
					t.Fatalf("第 %d 条历史应与原记录一致，want=%+v got=%+v",
						i, want, got[i])
				}
			}
			if w := s.Waiver("s1", "wok"); w == nil || w.Status != WaiverApproved {
				t.Fatalf("有效免修 wok 应可取且仍为有效，得到 %+v", w)
			}

			// 只读查询不应把记录标记为待保存，也不应改动原文件。
			if s.Dirty() {
				t.Fatal("只读核对与查询不应标记记录为已变更")
			}
			if b, readErr := os.ReadFile(path); readErr != nil || string(b) != string(raw) {
				t.Fatalf("只读查询后原文件应保持不变，readErr=%v", readErr)
			}

			// 重新保存再打开：有效来源、学分与完整历史仍然一致。
			if err := s.Save(path); err != nil {
				t.Fatalf("Save: %v", err)
			}
			reloaded, _, err := Load(path)
			if err != nil {
				t.Fatalf("重新保存后应可正常加载：%v", err)
			}
			rep2 := reloaded.CheckStudent("s1")
			if rep2.TotalCredits != 4 || len(rep2.Unmet) != 0 ||
				rep2.Requirements[0].Source != "waiver" ||
				rep2.Requirements[0].WaiverID != "wok" {
				t.Fatalf("重新打开后有效来源与学分应不变，得到 %+v", rep2)
			}
			if len(reloaded.Waivers("s1")) != 3 ||
				len(rep2.RejectedWaivers) != 1 || len(rep2.RevokedWaivers) != 1 {
				t.Fatalf("重新打开后三份历史均应保留，得到 %+v", reloaded.Waivers("s1"))
			}
		})
	}
}

// TestLoadAllowsSameWaiverAndReqIDsAcrossStudents 唯一性只作用于同一学生
// 名下的同一要求：不同学生各自使用相同的要求编号、甚至相同的免修编号，
// 只要各自只有一份有效申请，就应正常读取，不能误报为同一要求被重复取代。
// 两人的要求可以对应不同学分的课程，核对分别得到本人的课程学分与免修
// 来源；合法记录的只读查询保持文件内容不变。
func TestLoadAllowsSameWaiverAndReqIDsAcrossStudents(t *testing.T) {
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
		Enrollments: nil,
		Waivers: []*Waiver{
			{ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "学科竞赛获奖", Status: WaiverApproved},
			// s1 的失效历史：不能被算成第二份有效免修，也不能串到 s2。
			{ID: "w2", StudentID: "s1", ReqID: "r1", Basis: "重复提交依据",
				Status: WaiverRejected, Reason: "该要求已有有效免修 w1"},
			{ID: "w1", StudentID: "s2", ReqID: "r1", Basis: "外校修读证明", Status: WaiverApproved},
		},
	}
	path, raw := writeFileData(t, d)

	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("跨学生复用要求/免修编号应正常读取：existed=%v err=%v", existed, err)
	}

	rep1 := s.CheckStudent("s1")
	if rep1.TotalCredits != 4 || len(rep1.Unmet) != 0 {
		t.Fatalf("s1 应得到本人课程的 4 学分，得到学分=%d 未满足=%v",
			rep1.TotalCredits, rep1.Unmet)
	}
	st1 := rep1.Requirements[0]
	if st1.Course == nil || st1.Course.ID != "c1" || !st1.Satisfied ||
		st1.Source != "waiver" || st1.WaiverID != "w1" {
		t.Fatalf("s1 的来源应是本人 c1 上的 w1，得到 %+v", st1)
	}
	if len(rep1.RejectedWaivers) != 1 || rep1.RejectedWaivers[0].Waiver.ID != "w2" {
		t.Fatalf("s1 的失效历史应仍归 s1，得到 %+v", rep1.RejectedWaivers)
	}

	rep2 := s.CheckStudent("s2")
	if rep2.TotalCredits != 3 || len(rep2.Unmet) != 0 {
		t.Fatalf("s2 应得到本人课程的 3 学分，得到学分=%d 未满足=%v",
			rep2.TotalCredits, rep2.Unmet)
	}
	st2 := rep2.Requirements[0]
	if st2.Course == nil || st2.Course.ID != "c2" || !st2.Satisfied ||
		st2.Source != "waiver" || st2.WaiverID != "w1" {
		t.Fatalf("s2 的来源应是本人 c2 上的 w1，不能与 s1 的冲突混为一谈，得到 %+v", st2)
	}
	if len(rep2.RejectedWaivers) != 0 || len(rep2.RevokedWaivers) != 0 {
		t.Fatalf("s1 的失效历史不应出现在 s2 名下，得到 拒绝=%+v 撤销=%v",
			rep2.RejectedWaivers, rep2.RevokedWaivers)
	}

	// 同号免修按学生归属各取各的，互不借用。
	w1s1 := s.Waiver("s1", "w1")
	w1s2 := s.Waiver("s2", "w1")
	if w1s1 == nil || w1s2 == nil || w1s1 == w1s2 {
		t.Fatalf("两名学生的同号 w1 应是各自独立的记录，得到 %p %p", w1s1, w1s2)
	}
	if w1s1.Basis != "学科竞赛获奖" || w1s2.Basis != "外校修读证明" {
		t.Fatalf("两份同号免修应保留各自依据，得到 %+v %+v", w1s1, w1s2)
	}
	if len(s.Waivers("s1")) != 2 || len(s.Waivers("s2")) != 1 {
		t.Fatalf("免修历史应按学生隔离，s1=%+v s2=%+v", s.Waivers("s1"), s.Waivers("s2"))
	}

	// 合法记录的只读查询保持文件内容不变，也不标记为待保存。
	if s.Dirty() {
		t.Fatal("只读查询不应标记记录为已变更")
	}
	if b, readErr := os.ReadFile(path); readErr != nil || string(b) != string(raw) {
		t.Fatalf("只读查询后原文件应保持不变，readErr=%v", readErr)
	}
}
