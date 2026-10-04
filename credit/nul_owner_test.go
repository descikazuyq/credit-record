package credit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归“要求是否存在必须以所属学生和要求编号都完全一致为准”。
//
// 编号是 JSON 解码后的原文，本身允许含空字符 U+0000。旧式
// student + "\x00" + id 的拼接键会让
//
//	(学生 "s",       要求 "x\x00r")
//	(学生 "s\x00x",  要求 "r")
//
// 这两个属于不同学生的归属键拼成同一个字符串，进而：
//   - 读取时把两项合法要求误判为同一学生名下的重复要求，整份文件被拒绝；
//   - 免修查找目标要求时跨学生命中另一人的要求，借他人的要求获得学分。
//
// 修正方式是归属索引按 (学生, 编号) 两个字段逐一比较（ownerKey 结构体），
// 编号原文一律保留：不删除空字符、不改写编号、不合并记录。
const (
	nulOwnerA = "s"      // 学生甲
	nulOwnerB = "s\x00x" // 学生乙：编号解码后含一个空字符
	nulReqA   = "x\x00r" // 学生甲的要求
	nulReqB   = "r"      // 学生乙的要求（与甲的要求在旧拼接键下相撞）
)

// nulOwnersSetup 登记两名学生与两门学分为 4/3 的课程，并分别建立各自的要求。
// 两项要求在旧实现下会撞成同一项：要么第二个建不进去，要么读盘时被判重复。
func nulOwnersSetup(t *testing.T, s *Store) {
	t.Helper()
	mustCourse(t, s, "c4", "高等数学", 4)
	mustCourse(t, s, "c3", "线性代数", 3)
	mustStudent(t, s, nulOwnerA)
	mustStudent(t, s, nulOwnerB)
	mustReq(t, s, nulOwnerA, nulReqA, "c4")
	mustReq(t, s, nulOwnerB, nulReqB, "c3")
}

// assertNULRequirementsSeparate 核对两项要求各归各、编号原文（含空字符）保留。
func assertNULRequirementsSeparate(t *testing.T, s *Store) {
	t.Helper()
	ra := s.Requirement(nulOwnerA, nulReqA)
	if ra == nil {
		t.Fatalf("学生 %q 名下应存在要求 %q", nulOwnerA, nulReqA)
	}
	rb := s.Requirement(nulOwnerB, nulReqB)
	if rb == nil {
		t.Fatalf("学生 %q 名下应存在要求 %q", nulOwnerB, nulReqB)
	}
	if ra == rb {
		t.Fatal("两名学生的要求不应是同一条记录")
	}
	if ra.StudentID != nulOwnerA || ra.ID != nulReqA || ra.CourseID != "c4" {
		t.Fatalf("甲的要求归属或编号被改写：%+v", ra)
	}
	if rb.StudentID != nulOwnerB || rb.ID != nulReqB || rb.CourseID != "c3" {
		t.Fatalf("乙的要求归属或编号被改写：%+v", rb)
	}
	// 反向核对：跨学生查找必须查不到（同号要求只在别人名下不算）。
	if s.Requirement(nulOwnerA, nulReqB) != nil {
		t.Fatalf("学生 %q 不应能查到乙的要求 %q", nulOwnerA, nulReqB)
	}
	if s.Requirement(nulOwnerB, nulReqA) != nil {
		t.Fatalf("学生 %q 不应能查到甲的要求 %q", nulOwnerB, nulReqA)
	}
	if rs := s.Requirements(nulOwnerA); len(rs) != 1 || rs[0] != ra {
		t.Fatalf("学生 %q 应只有本人的一项要求，得到 %+v", nulOwnerA, rs)
	}
	if rs := s.Requirements(nulOwnerB); len(rs) != 1 || rs[0] != rb {
		t.Fatalf("学生 %q 应只有本人的一项要求，得到 %+v", nulOwnerB, rs)
	}
}

// TestNULIDRequirementsBelongToSeparateStudents 两项在旧拼接键下相撞的要求
// 必须分别属于两名学生：建得进、查得开、可分别免修、学分为各自课程学分，
// 保存重开后归属与历史仍各归各，编号原文（含空字符）完整保留。
func TestNULIDRequirementsBelongToSeparateStudents(t *testing.T) {
	s := NewStore()
	nulOwnersSetup(t, s)
	assertNULRequirementsSeparate(t, s)

	// 两人各提交一份指向本人要求的有效免修：编号同为 "w" 也互不影响。
	if w, a, err := s.ApplyWaiver(nulOwnerA, nulReqA, "w", "甲的竞赛获奖"); err != nil ||
		a != ActionCreated || w.Status != WaiverApproved {
		t.Fatalf("甲的免修应正常生效，得到 %+v action=%v err=%v", w, a, err)
	}
	if w, a, err := s.ApplyWaiver(nulOwnerB, nulReqB, "w", "乙的外校修读证明"); err != nil ||
		a != ActionCreated || w.Status != WaiverApproved {
		t.Fatalf("乙的免修应正常生效，得到 %+v action=%v err=%v", w, a, err)
	}

	assertNULCheckCredits := func(store *Store) {
		t.Helper()
		repA := store.CheckStudent(nulOwnerA)
		if repA.TotalCredits != 4 {
			t.Fatalf("甲应只获得本人课程的 4 学分，得到 %d", repA.TotalCredits)
		}
		if len(repA.Requirements) != 1 {
			t.Fatalf("甲应只有一项要求，得到 %+v", repA.Requirements)
		}
		stA := repA.Requirements[0]
		if !stA.Satisfied || stA.Source != "waiver" || stA.WaiverID != "w" ||
			stA.Req.ID != nulReqA || stA.Course == nil || stA.Course.ID != "c4" {
			t.Fatalf("甲的要求应由本人 w 满足并指向本人 4 学分课程，得到 %+v", stA)
		}
		if len(repA.RejectedWaivers) != 0 || len(repA.RevokedWaivers) != 0 {
			t.Fatalf("甲不应有被拒绝/已撤销免修，得到 %+v / %v",
				repA.RejectedWaivers, repA.RevokedWaivers)
		}
		repB := store.CheckStudent(nulOwnerB)
		if repB.TotalCredits != 3 {
			t.Fatalf("乙应只获得本人课程的 3 学分，得到 %d", repB.TotalCredits)
		}
		stB := repB.Requirements[0]
		if !stB.Satisfied || stB.Source != "waiver" || stB.WaiverID != "w" ||
			stB.Req.ID != nulReqB || stB.Course == nil || stB.Course.ID != "c3" {
			t.Fatalf("乙的要求应由本人 w 满足并指向本人 3 学分课程，得到 %+v", stB)
		}
		// 免修历史各保留本人的依据与原文目标要求（按内容核对，
		// 重新读盘后指针会变化）。
		wsA := store.Waivers(nulOwnerA)
		if len(wsA) != 1 || wsA[0].StudentID != nulOwnerA || wsA[0].ID != "w" ||
			wsA[0].ReqID != nulReqA || wsA[0].Basis != "甲的竞赛获奖" ||
			wsA[0].Status != WaiverApproved {
			t.Fatalf("甲的免修历史应只保留本人的申请，得到 %+v", wsA)
		}
		wsB := store.Waivers(nulOwnerB)
		if len(wsB) != 1 || wsB[0].StudentID != nulOwnerB || wsB[0].ID != "w" ||
			wsB[0].ReqID != nulReqB || wsB[0].Basis != "乙的外校修读证明" ||
			wsB[0].Status != WaiverApproved {
			t.Fatalf("乙的免修历史应只保留本人的申请，得到 %+v", wsB)
		}
	}
	assertNULCheckCredits(s)

	// 保存并重新打开：旧实现会在 Load 阶段把两项要求判为重复而拒绝整份文件。
	path := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// 落盘字节必须保留空字符（JSON 以 \u0000转义），证明没有改写编号。
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `\u0000`) {
		t.Fatalf("记录文件应保留编号中的空字符转义，raw=%q", raw)
	}
	loaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("两项分属不同学生的要求合法，文件应正常打开，existed=%v err=%v",
			existed, err)
	}
	assertNULRequirementsSeparate(t, loaded)
	assertNULCheckCredits(loaded)

	// 撤销后重开：已撤销免修仍必须引用本人真实要求，原依据保留、学分取消。
	if _, changed, err := loaded.RevokeWaiver(nulOwnerA, "w", "材料补充不上"); err != nil || !changed {
		t.Fatalf("撤销甲的 w 应成功，changed=%v err=%v", changed, err)
	}
	if _, changed, err := loaded.RevokeWaiver(nulOwnerB, "w", ""); err != nil || !changed {
		t.Fatalf("撤销乙的 w 应成功，changed=%v err=%v", changed, err)
	}
	if err := loaded.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("已撤销免修引用本人要求时文件应合法，existed=%v err=%v", existed, err)
	}
	for _, tc := range []struct {
		student string
		req     string
		basis   string
	}{
		{nulOwnerA, nulReqA, "甲的竞赛获奖"},
		{nulOwnerB, nulReqB, "乙的外校修读证明"},
	} {
		rep := reloaded.CheckStudent(tc.student)
		if rep.TotalCredits != 0 || len(rep.Unmet) != 1 || rep.Unmet[0] != tc.req {
			t.Fatalf("撤销后学生 %q 应 0 学分且要求 %q 未满足，得到 %+v",
				tc.student, tc.req, rep)
		}
		if len(rep.RevokedWaivers) != 1 || rep.RevokedWaivers[0] != "w" {
			t.Fatalf("学生 %q 应保留一条已撤销免修 w，得到 %+v",
				tc.student, rep.RevokedWaivers)
		}
		w := reloaded.Waiver(tc.student, "w")
		if w == nil || w.ReqID != tc.req || w.Basis != tc.basis ||
			w.Status != WaiverRevoked {
			t.Fatalf("学生 %q 的撤销历史应保留原文目标要求与原依据，得到 %+v",
				tc.student, w)
		}
	}
}

// TestNULIDEnrollmentsStaySeparate 修读归属索引存在同样的拼接缺陷：
// 两名学生编号/修读编号在旧键下相撞时，修读仍必须各归各、可分别提交通过、
// 学分分别来自各自课程。
func TestNULIDEnrollmentsStaySeparate(t *testing.T) {
	s := NewStore()
	nulOwnersSetup(t, s)
	// (学生 "s", 修读 "x\x00e") 与 (学生 "s\x00x", 修读 "e") 旧键相撞。
	mustEnroll(t, s, nulOwnerA, nulReqA, "2024春", "x\x00e")
	mustEnroll(t, s, nulOwnerB, nulReqB, "2024秋", "e")
	if _, changed, err := s.SubmitResult(nulOwnerA, "x\x00e", Passed); err != nil || !changed {
		t.Fatalf("甲的修读提交通过失败：changed=%v err=%v", changed, err)
	}
	if _, changed, err := s.SubmitResult(nulOwnerB, "e", Passed); err != nil || !changed {
		t.Fatalf("乙的修读提交通过失败：changed=%v err=%v", changed, err)
	}
	if s.Enrollment(nulOwnerA, "x\x00e") == nil || s.Enrollment(nulOwnerB, "e") == nil {
		t.Fatal("两名学生的同碰撞键修读都应能按归属查到")
	}
	if e := s.Enrollment(nulOwnerA, "e"); e != nil {
		t.Fatalf("甲不应查到乙的修读，得到 %+v", e)
	}

	path := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("含相撞键修读的合法文件应正常打开：%v", err)
	}
	if rep := loaded.CheckStudent(nulOwnerA); rep.TotalCredits != 4 ||
		len(rep.Requirements) != 1 || rep.Requirements[0].Source != "enrollment" ||
		rep.Requirements[0].PassedEnrollmentID != "x\x00e" {
		t.Fatalf("甲应凭本人修读获得 4 学分，得到 %+v", rep)
	}
	if rep := loaded.CheckStudent(nulOwnerB); rep.TotalCredits != 3 ||
		rep.Requirements[0].Source != "enrollment" ||
		rep.Requirements[0].PassedEnrollmentID != "e" {
		t.Fatalf("乙应凭本人修读获得 3 学分，得到 %+v", rep)
	}
}

// TestNULIDApprovedWaiverCannotBorrowOtherStudent 只有乙名下存在要求 "r"
// 时，甲提交一份声称取代 "x\x00r" 的免修：目标在甲名下不存在，必须拒绝，
// 不能借用乙的要求让它合法；乙本人的同号免修不受影响、照常获得 3 学分。
func TestNULIDApprovedWaiverCannotBorrowOtherStudent(t *testing.T) {
	s := NewStore()
	mustCourse(t, s, "c3", "线性代数", 3)
	mustStudent(t, s, nulOwnerA)
	mustStudent(t, s, nulOwnerB)
	mustReq(t, s, nulOwnerB, nulReqB, "c3") // 只有乙有要求

	w, a, err := s.ApplyWaiver(nulOwnerA, nulReqA, "w1", "声称符合的依据")
	if err != nil || a != ActionCreated {
		t.Fatalf("被拒绝的申请仍应新建为历史记录，得到 %+v action=%v err=%v", w, a, err)
	}
	if w.Status != WaiverRejected || w.StudentID != nulOwnerA || w.ReqID != nulReqA ||
		w.Basis != "声称符合的依据" || !strings.Contains(w.Reason, "不存在或不属于该学生") {
		t.Fatalf("甲的申请应以“目标要求不存在或不属于该学生”拒绝并保留原文，得到 %+v", w)
	}

	// 乙本人指向自己要求的免修必须照常生效，不能被甲的申请占用或干扰。
	wb, a, err := s.ApplyWaiver(nulOwnerB, nulReqB, "w1", "乙的依据")
	if err != nil || a != ActionCreated || wb.Status != WaiverApproved {
		t.Fatalf("乙的免修应正常生效，得到 %+v action=%v err=%v", wb, a, err)
	}
	repA := s.CheckStudent(nulOwnerA)
	if repA.TotalCredits != 0 || len(repA.Unmet) != 0 {
		t.Fatalf("甲没有要求也没有学分，得到 学分=%d 未满足=%v", repA.TotalCredits, repA.Unmet)
	}
	if len(repA.RejectedWaivers) != 1 || repA.RejectedWaivers[0].Waiver != w ||
		repA.RejectedWaivers[0].Waiver.ReqID != nulReqA {
		t.Fatalf("甲的核对应保留这条被拒绝申请及其原文目标，得到 %+v",
			repA.RejectedWaivers)
	}
	repB := s.CheckStudent(nulOwnerB)
	if repB.TotalCredits != 3 || repB.Requirements[0].WaiverID != "w1" {
		t.Fatalf("乙应凭本人有效免修获得 3 学分，得到 %+v", repB)
	}
}

// TestLoadNULIDApprovedWaiverMissingOwnRequirement 损坏文件场景：文件里只有
// 乙的要求，却有一份属于甲、目标为 "x\x00r" 的“有效”免修。旧实现会通过
// 拼接键借用乙的要求放行；正确行为是整份判为损坏，Load 失败且原文件不动。
func TestLoadNULIDApprovedWaiverMissingOwnRequirement(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c3", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: nulOwnerA}, {ID: nulOwnerB}},
		Requirements: []*Requirement{
			{ID: nulReqB, StudentID: nulOwnerB, CourseID: "c3"},
		},
		Waivers: []*Waiver{{
			ID: "w", StudentID: nulOwnerA, ReqID: nulReqA,
			Basis: "跨学生借用依据", Status: WaiverApproved,
		}},
	}
	path, raw := writeRecord(t, d)
	s, existed, err := Load(path)
	if err == nil {
		t.Fatalf("有效免修目标不属于该学生时必须按损坏拒绝，却得到 store=%v", s)
	}
	if !existed {
		t.Fatalf("损坏文件应标记为已存在，existed=%v err=%v", existed, err)
	}
	msg := err.Error()
	for _, want := range []string{"内容损坏", "w", "不属于该学生或不存在", path} {
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
}

// TestLoadNULIDRevokedWaiverNeedsOwnRequirement 已撤销免修同样必须保留对本人
// 真实要求的引用：只有乙的要求时，甲的已撤销免修不能靠拼接键借乙的要求放行。
func TestLoadNULIDRevokedWaiverNeedsOwnRequirement(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c3", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: nulOwnerA}, {ID: nulOwnerB}},
		Requirements: []*Requirement{
			{ID: nulReqB, StudentID: nulOwnerB, CourseID: "c3"},
		},
		Waivers: []*Waiver{{
			ID: "w", StudentID: nulOwnerA, ReqID: nulReqA,
			Basis: "甲曾经的依据", Status: WaiverRevoked, Reason: "材料无法核实",
		}},
	}
	path, raw := writeRecord(t, d)
	if s, _, err := Load(path); err == nil {
		t.Fatalf("已撤销免修目标不属于本人时必须按损坏拒绝，却得到 store=%v", s)
	} else if !strings.Contains(err.Error(), "目标要求无效") {
		t.Fatalf("错误应说明目标要求无效，得到：%v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatal("拒绝读取不得改写原文件")
	}
}

// TestLoadNULIDRejectedWaiverKeepsMissingTarget 已拒绝申请仍可记录当时不存在
// 的目标：甲的 rejected 申请（目标 "x\x00r" 在甲名下不存在）与乙本人要求的
// 有效免修共存时文件合法；打开后甲不得学分，拒绝原因与原文申请保留，
// 乙照常凭本人免修获得 3 学分。
func TestLoadNULIDRejectedWaiverKeepsMissingTarget(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c3", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: nulOwnerA}, {ID: nulOwnerB}},
		Requirements: []*Requirement{
			{ID: nulReqB, StudentID: nulOwnerB, CourseID: "c3"},
		},
		Waivers: []*Waiver{
			{
				ID: "wrej", StudentID: nulOwnerA, ReqID: nulReqA,
				Basis: "", Status: WaiverRejected,
				Reason: "目标要求 x\x00r 不存在或不属于该学生",
			},
			{
				ID: "wok", StudentID: nulOwnerB, ReqID: nulReqB,
				Basis: "乙的依据", Status: WaiverApproved,
			},
		},
	}
	path, _ := writeRecord(t, d)
	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("含不存在目标的已拒绝申请是合法历史，文件应打开，existed=%v err=%v",
			existed, err)
	}
	repA := s.CheckStudent(nulOwnerA)
	if repA.TotalCredits != 0 || !repA.Found || len(repA.Requirements) != 0 {
		t.Fatalf("甲应无要求无学分，得到 %+v", repA)
	}
	if len(repA.RejectedWaivers) != 1 {
		t.Fatalf("甲应保留 1 条被拒绝申请，得到 %+v", repA.RejectedWaivers)
	}
	rj := repA.RejectedWaivers[0].Waiver
	if rj.ID != "wrej" || rj.ReqID != nulReqA || rj.Status != WaiverRejected ||
		!strings.Contains(rj.Reason, "不存在") {
		t.Fatalf("拒绝记录应保留原文目标与拒绝原因，得到 %+v", rj)
	}
	if s.Waiver(nulOwnerA, "wrej") == nil {
		t.Fatal("被拒绝的申请应仍在甲的免修历史中可查")
	}
	repB := s.CheckStudent(nulOwnerB)
	if repB.TotalCredits != 3 || repB.Requirements[0].Source != "waiver" ||
		repB.Requirements[0].WaiverID != "wok" {
		t.Fatalf("乙应凭本人有效免修 wok 获得 3 学分，得到 %+v", repB)
	}
	if len(repB.RejectedWaivers) != 0 {
		t.Fatalf("甲的拒绝申请不能串到乙的历史，得到 %+v", repB.RejectedWaivers)
	}
}

// TestNULIDSameStudentDuplicatesStillRejected 纠正归属判定后，同一学生名下
// 真正重复的要求仍按原规则拒绝（内存路径与读盘路径都查），不同学生使用
// 同号要求（含空字符边界）仍合法。
func TestNULIDSameStudentDuplicatesStillRejected(t *testing.T) {
	s := NewStore()
	nulOwnersSetup(t, s)

	// 同学生、同要求编号、同课程：幂等返回原记录。
	if r, a, err := s.AddRequirement(nulOwnerA, nulReqA, "c4"); err != nil ||
		a != ActionExisted || r.CourseID != "c4" {
		t.Fatalf("同一学生重复登记相同要求应幂等，得到 %+v action=%v err=%v", r, a, err)
	}
	// 同学生、同要求编号改指另一课程：拒绝。
	if _, _, err := s.AddRequirement(nulOwnerA, nulReqA, "c3"); err == nil {
		t.Fatal("同一学生同号要求改指其他课程必须拒绝")
	}
	// 同学生就同一课程换编号再建：拒绝。
	if _, _, err := s.AddRequirement(nulOwnerA, "other", "c4"); err == nil {
		t.Fatal("同一学生就同一课程重复建立要求必须拒绝")
	}

	// 不同学生使用完全相同的要求编号仍合法（其中一人编号含空字符）。
	// 两人各自已有一门课的要求，所以同号新要求指向各自尚未引用的课程。
	mustCourse(t, s, "c5", "概率论", 2)
	mustCourse(t, s, "c6", "离散数学", 2)
	mustReq(t, s, nulOwnerA, "shared", "c5")
	mustReq(t, s, nulOwnerB, "shared", "c6")
	if s.Requirement(nulOwnerA, "shared").CourseID != "c5" ||
		s.Requirement(nulOwnerB, "shared").CourseID != "c6" {
		t.Fatal("不同学生的同号要求应分别指向各自课程")
	}

	// 读盘路径：同一学生两条同号要求（编号含空字符）必须判损坏。
	bad := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c4", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c3", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: nulOwnerA}},
		Requirements: []*Requirement{
			{ID: nulReqA, StudentID: nulOwnerA, CourseID: "c4"},
			{ID: nulReqA, StudentID: nulOwnerA, CourseID: "c3"},
		},
	}
	path, _ := writeRecord(t, bad)
	if _, _, err := Load(path); err == nil {
		t.Fatal("同一学生名下编号完全相同的两条要求必须按重复拒绝")
	}
}
