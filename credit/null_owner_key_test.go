package credit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归“要求是否存在必须以所属学生和要求编号都完全一致为准”。
//
// 旧实现把复合键拼成 student + "\x00" + id 作为 map 键。编号本身允许是
// JSON 解码后的任意文字，含 U+0000 也合法；于是 ("s", "x\x00r") 与
// ("s\x00x", "r") 两个不同归属会生成同一个拼接键，造成：
//   - 两名学生各自合法的要求/修读/免修在读取时互相顶替，被判重复而拒绝整份
//     合法文件；
//   - 某学生的有效免修找不到本人要求时，顺着冲突键借用另一名学生的同键要求
//     变得“合法”，核对串到他人的课程学分。
//
// 修复后复合键是两字段结构体，编号原文（含空字符）逐字保留，以下用例固定
// 这些结论。

// nulOwnersRecord 构造题目所述两名学生的完整合法记录：
//   - 学生 "s"：要求 "x\x00r" 指向 4 学分课程 c4，有一条通过修读与一份
//     有效免修；
//   - 学生 "s\x00x"：要求 "r" 指向 3 学分课程 c3，另有一条通过修读与一份
//     有效免修。
//
// 修读与免修编号同样取成会在旧拼接键下互相冲突的文字（("s","x\x00e") 对
// ("s\x00x","e")、("s","x\x00w") 对 ("s\x00x","w")），一并固定归属隔离。
func nulOwnersRecord() *fileData {
	return &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c4", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c3", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s"}, {ID: "s\x00x"}},
		Requirements: []*Requirement{
			{ID: "x\x00r", StudentID: "s", CourseID: "c4"},
			{ID: "r", StudentID: "s\x00x", CourseID: "c3"},
		},
		Enrollments: []*Enrollment{
			{ID: "x\x00e", StudentID: "s", ReqID: "x\x00r", Term: "2024春",
				Result: Passed, ResultSeq: 1},
			{ID: "e", StudentID: "s\x00x", ReqID: "r", Term: "2024春",
				Result: Passed, ResultSeq: 2},
		},
		Waivers: []*Waiver{
			{ID: "x\x00w", StudentID: "s", ReqID: "x\x00r",
				Basis: "s 的竞赛获奖", Status: WaiverApproved},
			{ID: "w", StudentID: "s\x00x", ReqID: "r",
				Basis: "s\x00x 的外校修读证明", Status: WaiverApproved},
		},
		NextResultSeq: 2,
	}
}

// assertNulOwnersSeparated 核对两名学生的要求、修读、免修与学分完全各归各。
func assertNulOwnersSeparated(t *testing.T, s *Store) {
	t.Helper()

	// 要求查询：本人的查得到，对方的同拼接键要求查不到，指针互不相同。
	rS := s.Requirement("s", "x\x00r")
	rSX := s.Requirement("s\x00x", "r")
	if rS == nil || rSX == nil || rS == rSX {
		t.Fatalf("两名学生的要求应各自独立存在，得到 %p 与 %p", rS, rSX)
	}
	if rS.CourseID != "c4" || rSX.CourseID != "c3" {
		t.Fatalf("要求课程归属错误：s 应指 c4、s\\x00x 应指 c3，得到 %s、%s",
			rS.CourseID, rSX.CourseID)
	}
	if got := s.Requirement("s", "r"); got != nil {
		t.Fatalf("学生 s 名下不存在要求 r（那是 s\\x00x 的），却得到 %+v", got)
	}
	if got := s.Requirement("s\x00x", "x\x00r"); got != nil {
		t.Fatalf("学生 s\\x00x 名下不存在要求 x\\x00r（那是 s 的），却得到 %+v", got)
	}
	if reqs := s.AllRequirements(); len(reqs) != 2 {
		t.Fatalf("应保留两项要求，不得合并，得到 %d 项：%+v", len(reqs), reqs)
	}

	// 修读与免修编号同样按所属学生隔离，交叉查询必须全部落空。
	if e := s.Enrollment("s", "x\x00e"); e == nil || e.ReqID != "x\x00r" {
		t.Fatalf("s 的修读 x\\x00e 应指向本人要求，得到 %+v", e)
	}
	if e := s.Enrollment("s\x00x", "e"); e == nil || e.ReqID != "r" {
		t.Fatalf("s\\x00x 的修读 e 应指向本人要求 r，得到 %+v", e)
	}
	if s.Enrollment("s", "e") != nil || s.Enrollment("s\x00x", "x\x00e") != nil {
		t.Fatal("修读编号交叉查询不应串到另一名学生")
	}
	wS := s.Waiver("s", "x\x00w")
	wSX := s.Waiver("s\x00x", "w")
	if wS == nil || wSX == nil || wS == wSX {
		t.Fatalf("两名学生的有效免修应各自独立，得到 %p 与 %p", wS, wSX)
	}
	if s.Waiver("s", "w") != nil || s.Waiver("s\x00x", "x\x00w") != nil {
		t.Fatal("免修编号交叉查询不应串到另一名学生")
	}

	// 核对结果：s 凭本人有效免修得 4 学分，s\x00x 得 3 学分；有有效免修时
	// 以免修说明来源，通过修读只作历史、不重复计学分。
	rep := s.CheckStudent("s")
	if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
		t.Fatalf("s 应凭本人免修满足要求、计 4 学分，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	if len(rep.Requirements) != 1 {
		t.Fatalf("s 名下应只有一项要求，得到 %+v", rep.Requirements)
	}
	st := rep.Requirements[0]
	if st.Req.ID != "x\x00r" || st.Course.ID != "c4" || st.Course.Credit != 4 ||
		st.Source != "waiver" || st.WaiverID != "x\x00w" ||
		len(st.PassedEnrollmentIDs) != 1 || st.PassedEnrollmentIDs[0] != "x\x00e" {
		t.Fatalf("s 的核对依据应全部来自本人记录，得到 %+v（历史 %v）",
			st, st.PassedEnrollmentIDs)
	}
	if len(rep.RejectedWaivers) != 0 || len(rep.RevokedWaivers) != 0 {
		t.Fatalf("s 不应有失效免修历史，得到 %+v / %v",
			rep.RejectedWaivers, rep.RevokedWaivers)
	}

	rep = s.CheckStudent("s\x00x")
	if rep.TotalCredits != 3 || len(rep.Unmet) != 0 {
		t.Fatalf("s\\x00x 应凭本人免修满足要求、计 3 学分，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	if len(rep.Requirements) != 1 {
		t.Fatalf("s\\x00x 名下应只有一项要求，得到 %+v", rep.Requirements)
	}
	st = rep.Requirements[0]
	if st.Req.ID != "r" || st.Course.ID != "c3" || st.Course.Credit != 3 ||
		st.Source != "waiver" || st.WaiverID != "w" ||
		len(st.PassedEnrollmentIDs) != 1 || st.PassedEnrollmentIDs[0] != "e" {
		t.Fatalf("s\\x00x 的核对依据应全部来自本人记录，得到 %+v（历史 %v）",
			st, st.PassedEnrollmentIDs)
	}

	// 免修历史各自只含本人申请，查看历史保留含空字符的编号原文与依据。
	ws := s.Waivers("s")
	if len(ws) != 1 || ws[0] != wS || ws[0].ReqID != "x\x00r" ||
		ws[0].Basis != "s 的竞赛获奖" {
		t.Fatalf("s 的免修历史应只保留本人 x\\x00w 申请，得到 %+v", ws)
	}
	wsx := s.Waivers("s\x00x")
	if len(wsx) != 1 || wsx[0] != wSX || wsx[0].ReqID != "r" ||
		wsx[0].Basis != "s\x00x 的外校修读证明" {
		t.Fatalf("s\\x00x 的免修历史应只保留本人 w 申请，得到 %+v", wsx)
	}
}

// TestLoadNullContainingIDsKeepOwnership 题目主场景：含 U+0000 的两组
// 归属在旧拼接键下完全同键，合法文件必须正常打开，两人核对分别得到
// 4 学分与 3 学分；保存后重开编号原文与归属全部保留。
func TestLoadNullContainingIDsKeepOwnership(t *testing.T) {
	path, raw := writeRecord(t, nulOwnersRecord())

	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("两名学生的合法记录必须正常读取，不得误报要求/免修编号重复，existed=%v err=%v",
			existed, err)
	}
	assertNulOwnersSeparated(t, s)

	// 只读核对不得改动文件。
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != string(raw) {
		t.Fatalf("只读访问不得改写原文件，readErr=%v", readErr)
	}

	// 重新保存再加载：空字符必须逐字保留，不能为消除冲突删改编号或合并记录。
	path2 := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path2); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, existed, err := Load(path2)
	if err != nil || !existed {
		t.Fatalf("含空字符编号的记录重存后应可再次加载，existed=%v err=%v", existed, err)
	}
	assertNulOwnersSeparated(t, reloaded)
	for _, r := range reloaded.AllRequirements() {
		if !strings.Contains(r.StudentID+r.ID, "\x00") {
			t.Fatalf("重存后编号原文中的空字符丢失：%+v", r)
		}
	}
}

// TestLoadApprovedWaiverCannotBorrowNullConfusableRequirement 文件里只有
// 学生 "s\x00x" 名下的要求 "r"，却有一份属于 "s"、声称取代 "x\x00r" 的
// 有效免修：旧实现顺着相同拼接键借用第二名学生的要求让它合法。修复后
// 必须明确报告免修目标不属于该学生或不存在，按损坏文件处理，原文件保留。
func TestLoadApprovedWaiverCannotBorrowNullConfusableRequirement(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c4", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c3", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s"}, {ID: "s\x00x"}},
		Requirements: []*Requirement{
			// 只有第二名学生的要求 r -> c3；s 名下没有任何要求。
			{ID: "r", StudentID: "s\x00x", CourseID: "c3"},
		},
		Waivers: []*Waiver{
			// s\x00x 本人的有效免修：合法。
			{ID: "w", StudentID: "s\x00x", ReqID: "r",
				Basis: "s\x00x 本人的依据", Status: WaiverApproved},
			// s 声称取代 x\x00r：s 名下不存在该要求，不能借用 s\x00x 的 r。
			{ID: "x\x00w", StudentID: "s", ReqID: "x\x00r",
				Basis: "s 的材料", Status: WaiverApproved},
		},
	}
	path, raw := writeRecord(t, d)

	s, existed, err := Load(path)
	if err == nil {
		t.Fatalf("有效免修借用他人同键要求时必须整份拒绝，却得到 store=%v", s)
	}
	if !existed {
		t.Fatalf("应按已有损坏文件处理（existed=true），existed=%v", existed)
	}
	msg := err.Error()
	for _, want := range []string{"内容损坏", "s", "x\x00w", "x\x00r", "不存在或不属于该学生"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误应明确报告目标要求不属于该学生或不存在（缺 %q），得到：%v", want, err)
		}
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != string(raw) {
		t.Fatalf("拒绝读取不得改写原文件，readErr=%v", readErr)
	}
}

// TestLoadNullConfusableRejectedWaiverKeptAsHistory 被拒绝的申请仍可记录
// 不存在的目标：s 一份指向 "x\x00r"（s 名下并无此要求，只有 s\x00x 的 r）
// 的 rejected 申请必须原样保留（编号、依据、原因），不获得学分，也不变成
// 有效免修；s\x00x 本人的核对不受影响。
func TestLoadNullConfusableRejectedWaiverKeptAsHistory(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c3", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s"}, {ID: "s\x00x"}},
		Requirements: []*Requirement{
			{ID: "r", StudentID: "s\x00x", CourseID: "c3"},
		},
		Waivers: []*Waiver{
			{ID: "w", StudentID: "s\x00x", ReqID: "r",
				Basis: "s\x00x 本人的依据", Status: WaiverApproved},
			{ID: "x\x00w", StudentID: "s", ReqID: "x\x00r", Basis: "s 的材料",
				Status: WaiverRejected, Reason: "目标要求 x\x00r 不存在或不属于该学生"},
		},
	}
	path, _ := writeRecord(t, d)

	s, _, err := Load(path)
	if err != nil {
		t.Fatalf("被拒绝申请允许指向不存在的目标，记录应正常打开：%v", err)
	}

	rep := s.CheckStudent("s")
	if rep.TotalCredits != 0 || len(rep.Unmet) != 0 || len(rep.Requirements) != 0 {
		// s 名下没有要求：没有未满足项，也没有学分。
		t.Fatalf("s 无要求无有效免修，应 0 学分，得到 %+v", rep)
	}
	if len(rep.RejectedWaivers) != 1 {
		t.Fatalf("s 的被拒绝申请应保留在历史中，得到 %+v", rep.RejectedWaivers)
	}
	rj := rep.RejectedWaivers[0].Waiver
	if rj.ID != "x\x00w" || rj.ReqID != "x\x00r" || rj.Basis != "s 的材料" ||
		rj.Status != WaiverRejected {
		t.Fatalf("被拒绝申请应原样保留编号/目标/依据，得到 %+v", rj)
	}

	// s\x00x 本人的有效免修与 3 学分不受 s 的失效历史影响。
	rep2 := s.CheckStudent("s\x00x")
	if rep2.TotalCredits != 3 || len(rep2.RejectedWaivers) != 0 {
		t.Fatalf("s\\x00x 应仍凭本人 w 得 3 学分且无拒绝历史，得到 %+v", rep2)
	}
}

// TestLoadNullConfusableRevokedWaiverStillNeedsOwnRequirement 已撤销免修
// 同样必须保留对本人真实要求的引用：s 的已撤销免修指向并不存在于 s 名下的
// "x\x00r"（只有 s\x00x 的 r）时，整份文件判损坏，不能借用他人要求合法化。
func TestLoadNullConfusableRevokedWaiverStillNeedsOwnRequirement(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c3", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s"}, {ID: "s\x00x"}},
		Requirements: []*Requirement{
			{ID: "r", StudentID: "s\x00x", CourseID: "c3"},
		},
		Waivers: []*Waiver{
			{ID: "x\x00w", StudentID: "s", ReqID: "x\x00r", Basis: "s 的旧材料",
				Status: WaiverRevoked, Reason: "材料无法核实"},
		},
	}
	path, raw := writeRecord(t, d)

	s, existed, err := Load(path)
	if err == nil {
		t.Fatalf("已撤销免修借用他人同键要求时必须整份拒绝，却得到 store=%v", s)
	}
	if !existed {
		t.Fatalf("应按已有损坏文件处理，existed=%v", existed)
	}
	msg := err.Error()
	if !strings.Contains(msg, "内容损坏") ||
		!strings.Contains(msg, "目标要求无效") || !strings.Contains(msg, "x\x00r") {
		t.Fatalf("错误应说明已撤销免修的目标要求在本人名下无效，得到：%v", err)
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != string(raw) {
		t.Fatalf("拒绝读取不得改写原文件，readErr=%v", readErr)
	}
}

// TestNullContainingIDsInMemoryOwnership 经正常登记/申请流程产生的含空字符
// 编号同样按归属隔离：两项要求各自新建成功（不是幂等命中也不是重复拒绝），
// 各自的有效免修只认本人要求；顺着冲突键替他人要求申请免修必须被拒绝，
// 同一学生名下真正的重复要求仍按原规则拒绝。
func TestNullContainingIDsInMemoryOwnership(t *testing.T) {
	s := NewStore()
	mustCourse(t, s, "c4", "高等数学", 4)
	mustCourse(t, s, "c3", "线性代数", 3)
	mustStudent(t, s, "s")
	mustStudent(t, s, "s\x00x")

	r1, a, err := s.AddRequirement("s", "x\x00r", "c4")
	if err != nil || a != ActionCreated {
		t.Fatalf("s 登记 x\\x00r 应新建成功，得到 %+v action=%v err=%v", r1, a, err)
	}
	r2, a, err := s.AddRequirement("s\x00x", "r", "c3")
	if err != nil || a != ActionCreated {
		t.Fatalf("s\\x00x 登记 r 应是独立的新建，不能与 s 的 x\\x00r 混为一项，得到 %+v action=%v err=%v",
			r2, a, err)
	}
	if r1 == r2 {
		t.Fatal("两项要求应是不同记录")
	}

	// 各自重复登记本人要求：幂等返回原记录。
	if got, a, err := s.AddRequirement("s", "x\x00r", "c4"); err != nil ||
		a != ActionExisted || got != r1 {
		t.Fatalf("s 重复登记本人要求应幂等，得到 %+v action=%v err=%v", got, a, err)
	}
	if got, a, err := s.AddRequirement("s\x00x", "r", "c3"); err != nil ||
		a != ActionExisted || got != r2 {
		t.Fatalf("s\\x00x 重复登记本人要求应幂等，得到 %+v action=%v err=%v", got, a, err)
	}

	// 同一学生就同一课程换编号再建要求：仍按原规则拒绝。
	if _, _, err := s.AddRequirement("s", "other", "c4"); err == nil ||
		!strings.Contains(err.Error(), "重复建立") {
		t.Fatalf("同一学生就同一课程重复建立要求应拒绝，得到 %v", err)
	}

	// s\x00x 不能替 s 的 x\x00r 申请免修：旧实现会顺着同键要求误判目标存在。
	w, a, err := s.ApplyWaiver("s\x00x", "x\x00r", "wbad", "借来的依据")
	if err != nil || a != ActionCreated || w.Status != WaiverRejected {
		t.Fatalf("替他人同键要求申请免修应被拒绝并留历史，得到 %+v action=%v err=%v", w, a, err)
	}
	if !strings.Contains(w.Reason, "不存在或不属于该学生") {
		t.Fatalf("拒绝原因应说明目标要求不属于该学生，得到 %q", w.Reason)
	}

	// 两人各自指向本人要求的免修都正常生效，核对分别得到本人课程学分。
	if w, a, err := s.ApplyWaiver("s", "x\x00r", "x\x00w", "s 的依据"); err != nil ||
		a != ActionCreated || w.Status != WaiverApproved {
		t.Fatalf("s 为本人 x\\x00r 申请免修应生效，得到 %+v action=%v err=%v", w, a, err)
	}
	if w, a, err := s.ApplyWaiver("s\x00x", "r", "w", "s\x00x 的依据"); err != nil ||
		a != ActionCreated || w.Status != WaiverApproved {
		t.Fatalf("s\\x00x 为本人 r 申请免修应生效，得到 %+v action=%v err=%v", w, a, err)
	}
	if rep := s.CheckStudent("s"); rep.TotalCredits != 4 ||
		len(rep.RejectedWaivers) != 0 {
		t.Fatalf("s 应只有本人要求的 4 学分，被拒绝的是 s\\x00x 的申请，得到 %+v", rep)
	}
	if rep := s.CheckStudent("s\x00x"); rep.TotalCredits != 3 {
		t.Fatalf("s\\x00x 应只有本人要求的 3 学分，其被拒绝申请不计学分，得到 %+v", rep)
	}
}
