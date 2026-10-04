package credit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归“读取已有记录时，有效免修的依据校验与首次正常申请一致”：
// 状态为有效（approved）的免修，依据必须含有实际文字；依据为空，或全部
// 由空白字符（空格、制表符、换行、回车、全角空格 U+3000、不换行空格
// U+00A0 等，允许混用）组成时，整份记录按内容损坏拒绝读取，即使该要求
// 另有通过修读也不能绕过。已拒绝申请的依据允许为空或只含空白；依据含
// 实际文字时其前后与中间的空白必须原样保留，读取不得修剪或改写。
//
// 这类矛盾无法经正常申请流程产生（空白依据的首次申请会被业务拒绝并记为
// rejected），因此损坏用例直接构造记录文件，聚焦 Load 读取历史这一路径。

// blankBasisBase 构造结构完整、引用齐全的基础记录：学生 s1 的要求 r1
// 指向 4 学分课程 c1。withPass 时再附一条已通过修读 e1。
func blankBasisBase(withPass bool) *fileData {
	d := &fileData{
		Version:      recordVersion,
		Courses:      []*Course{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
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

// TestLoadRejectsApprovedWaiverWithBlankBasis 有效免修依据为空或全部为空白
// 字符时，无论空白由哪些字符、以什么方式混合组成，Load 都必须判为内容
// 损坏：错误点名记录文件、所属学生与免修编号，文件标记为已存在，原文件
// 逐字节保留，且不能返回可用于核对的 store。
func TestLoadRejectsApprovedWaiverWithBlankBasis(t *testing.T) {
	blanks := map[string]string{
		"空字符串":        "",
		"普通空格":        "     ",
		"制表符换行回车":     "\t\n\r\n\t",
		"全角空格":        "　　　",
		"不换行空格":       "     ",
		"混合空白":        " \t　 \n  \r　",
		"空白夹在前后无实际文字": "\n\t　  ",
	}
	for name, basis := range blanks {
		t.Run(name, func(t *testing.T) {
			d := blankBasisBase(false)
			d.Waivers = []*Waiver{{
				ID: "w1", StudentID: "s1", ReqID: "r1",
				Basis: basis, Status: WaiverApproved,
			}}
			path, raw := writeRecord(t, d)

			s, existed, err := Load(path)
			if err == nil {
				t.Fatalf("有效免修依据为空白（%s）应判内容损坏拒绝读取，却得到 store=%v", name, s)
			}
			if !existed {
				t.Fatalf("损坏文件应标记为已存在，existed=%v err=%v", existed, err)
			}
			msg := err.Error()
			for _, want := range []string{path, "内容损坏", "s1", "w1", "有效免修", "依据"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("错误信息应包含 %q（文件/学生/免修编号/缺少依据），得到：%v",
						want, err)
				}
			}

			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != string(raw) {
				t.Fatalf("拒绝读取不得改写原文件\nwant=%q\n got=%q", raw, got)
			}
		})
	}
}

// TestLoadBlankBasisApprovedNotMaskedByPassedEnrollment 即使该要求另有通过
// 修读（本来足以解释学分、满足要求），有效免修缺少依据仍然让整份记录
// 损坏：不能拿着通过记录继续核对学分、输出总学分或要求已满足。
func TestLoadBlankBasisApprovedNotMaskedByPassedEnrollment(t *testing.T) {
	d := blankBasisBase(true)
	d.Waivers = []*Waiver{{
		ID: "w1", StudentID: "s1", ReqID: "r1",
		Basis: " 　\t \n ", Status: WaiverApproved,
	}}
	path, raw := writeRecord(t, d)

	s, existed, err := Load(path)
	if err == nil {
		t.Fatalf("另有通过修读也不能放过缺少依据的有效免修，得到 store=%v", s)
	}
	if !existed {
		t.Fatalf("应标记文件已存在，existed=%v err=%v", existed, err)
	}
	if msg := err.Error(); !strings.Contains(msg, "内容损坏") ||
		!strings.Contains(msg, "s1") || !strings.Contains(msg, "w1") {
		t.Fatalf("错误应点名 s1 的有效免修 w1 缺少依据，得到：%v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(raw) {
		t.Fatal("拒绝读取不得改写原文件")
	}
}

// TestLoadBlankBasisAttributesCorrectStudent 文件中另有完全合法的学生 s2
// （本人的课程、要求与一份依据齐全的有效免修）时，s1 名下缺少依据的有效
// 免修仍让整份文件不可读；错误必须归属 s1，不能误报 s2 的合法记录。
func TestLoadBlankBasisAttributesCorrectStudent(t *testing.T) {
	d := blankBasisBase(false)
	d.Courses = append(d.Courses, &Course{ID: "c2", Name: "线性代数", Credit: 3, Open: true})
	d.Students = append(d.Students, &Student{ID: "s2"})
	d.Requirements = append(d.Requirements, &Requirement{ID: "r1", StudentID: "s2", CourseID: "c2"})
	d.Waivers = []*Waiver{
		{ID: "w1", StudentID: "s1", ReqID: "r1", Basis: "　", Status: WaiverApproved},
		{ID: "w1", StudentID: "s2", ReqID: "r1", Basis: "s2 本人的依据", Status: WaiverApproved},
	}
	path, raw := writeRecord(t, d)

	s, _, err := Load(path)
	if err == nil {
		t.Fatalf("s1 名下有效免修缺少依据时必须整份拒绝，得到 store=%v", s)
	}
	msg := err.Error()
	if !strings.Contains(msg, "s1") || !strings.Contains(msg, "w1") {
		t.Fatalf("错误应归属学生 s1 及其免修 w1，得到：%v", err)
	}
	if strings.Contains(msg, "学生 s2") {
		t.Fatalf("s2 的免修依据齐全，不应被点名为问题记录，得到：%v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(raw) {
		t.Fatal("拒绝读取不得改写原文件")
	}
}

// TestLoadRejectedWaiverBlankBasisStillLoads 已拒绝申请的依据允许为空或只
// 含空白（含全角空格、不换行空格）：记录正常读取，拒绝状态与原因照常
// 保留、核对中可查，但不获得学分；文件不因此判坏。
func TestLoadRejectedWaiverBlankBasisStillLoads(t *testing.T) {
	d := blankBasisBase(true) // 有通过修读，r1 由通过记录满足
	d.Waivers = []*Waiver{
		{ID: "w0", StudentID: "s1", ReqID: "ghost", Basis: "",
			Status: WaiverRejected, Reason: "目标要求 ghost 不存在或不属于该学生"},
		{ID: "wr", StudentID: "s1", ReqID: "r1", Basis: " \t　 \n ",
			Status: WaiverRejected, Reason: "免修依据为空"},
	}
	path, raw := writeRecord(t, d)

	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("已拒绝申请依据为空白不应判坏文件，existed=%v err=%v", existed, err)
	}
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
		t.Fatalf("r1 应由通过修读满足、计 4 学分，得到 学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	if st := rep.Requirements[0]; st.Source != "enrollment" || st.WaiverID != "" {
		t.Fatalf("被拒绝的空白依据申请不能成为来源，得到 %+v", st)
	}
	if len(rep.RejectedWaivers) != 2 {
		t.Fatalf("两条被拒绝历史都应保留，得到 %d 条", len(rep.RejectedWaivers))
	}
	for _, rj := range rep.RejectedWaivers {
		if rj.Waiver.Status != WaiverRejected || rj.Reason == "" {
			t.Fatalf("被拒绝记录应保留状态与原因，得到 %+v", rj)
		}
	}
	// 只读核对不得改动文件。
	if s.Dirty() {
		t.Fatal("只读核对不应标记变更")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(raw) {
		t.Fatal("合法文件只读访问后内容必须保持不变")
	}
}

// TestLoadApprovedWaiverBasisWhitespacePreserved 依据含有实际文字时，前后
// 与中间的空白（缩进、换行、全角空格、不换行空格）都是材料的一部分：
// 记录必须正常读取，依据原文逐字符保留在免修历史中，核对以免修满足要求，
// 重新保存再加载后依据仍与原文件一致，读取/保存均不得修剪或改写。
func TestLoadApprovedWaiverBasisWhitespacePreserved(t *testing.T) {
	basis := "\t\n  学科竞赛获奖证明　 \n  获奖证书编号：001  \n  "
	d := blankBasisBase(false)
	d.Waivers = []*Waiver{{
		ID: "w1", StudentID: "s1", ReqID: "r1",
		Basis: basis, Status: WaiverApproved,
	}}
	path, raw := writeRecord(t, d)

	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("含实际文字（带缩进与换行）的依据应正常读取，existed=%v err=%v",
			existed, err)
	}
	w := s.Waiver("s1", "w1")
	if w == nil || w.Status != WaiverApproved {
		t.Fatalf("免修应保持有效，得到 %+v", w)
	}
	if w.Basis != basis {
		t.Fatalf("依据中的前后/中间空白必须原样保留，\nwant=%q\n got=%q", basis, w.Basis)
	}
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
		t.Fatalf("r1 应由该有效免修满足、计 4 学分，得到 学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	if st := rep.Requirements[0]; st.Source != "waiver" || st.WaiverID != "w1" {
		t.Fatalf("来源应是有效免修 w1，得到 %+v", st)
	}

	// 重新保存后文件字节应与原文件一致（依据未被修剪），再次加载结论不变。
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	saved, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(saved) != string(raw) {
		t.Fatalf("重新保存不得修剪或改写依据\nwant=%q\n got=%q", raw, saved)
	}
	reloaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("重新保存后应可再次加载：%v", err)
	}
	if w2 := reloaded.Waiver("s1", "w1"); w2 == nil || w2.Basis != basis {
		t.Fatalf("再次加载后依据原文应保持不变，得到 %+v", w2)
	}
}

// TestLoadRevokedWaiverBlankBasisStillRejected 已撤销免修的现有校验保持
// 不变：原依据全部为空白（含全角空格、不换行空格）同样判内容损坏。
func TestLoadRevokedWaiverBlankBasisStillRejected(t *testing.T) {
	d := blankBasisBase(false)
	d.Waivers = []*Waiver{{
		ID: "wv", StudentID: "s1", ReqID: "r1",
		Basis: " 　\t ", Status: WaiverRevoked, Reason: "材料无法核实",
	}}
	path, raw := writeRecord(t, d)
	if _, _, err := Load(path); err == nil {
		t.Fatal("已撤销免修原依据全为空白时仍应判内容损坏")
	} else if msg := err.Error(); !strings.Contains(msg, "内容损坏") ||
		!strings.Contains(msg, "s1") || !strings.Contains(msg, "wv") {
		t.Fatalf("错误应点名 s1 的已撤销免修 wv，得到：%v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(raw) {
		t.Fatal("拒绝读取不得改写原文件")
	}
}

// TestApplyWaiverBlankBasisBusinessRejectionUnchanged 首次正常提交空白依据
// 申请的行为保持不变：按业务规则拒绝（退出码语义在命令行层体现为 1），
// 申请内容与拒绝原因保留为 rejected 历史；保存后重新打开是合法记录，
// 拒绝历史可查且不获得学分——不会被新的读取校验反过来判成文件损坏。
func TestApplyWaiverBlankBasisBusinessRejectionUnchanged(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "数学", 4)
	mustReq(t, s, "s1", "r1", "c1")

	w, action, err := s.ApplyWaiver("s1", "r1", "w-empty", " \t　 \n  ")
	if err != nil || action != ActionCreated {
		t.Fatalf("空白依据首次申请不是硬错误，应新建一条被拒绝记录，得到 %+v action=%v err=%v",
			w, action, err)
	}
	if w.Status != WaiverRejected || w.Reason != "免修依据为空" {
		t.Fatalf("空白依据应按业务规则拒绝并保留原因，得到 %+v", w)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "records.json")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("含被拒绝空白依据申请的文件应正常打开，existed=%v err=%v", existed, err)
	}
	w2 := reloaded.Waiver("s1", "w-empty")
	if w2 == nil || w2.Status != WaiverRejected || w2.Reason != "免修依据为空" {
		t.Fatalf("重新打开后被拒绝记录与原因应保留，得到 %+v", w2)
	}
	rep := reloaded.CheckStudent("s1")
	if rep.TotalCredits != 0 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
		t.Fatalf("被拒绝的空白依据申请不获得学分，得到 学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	if len(rep.RejectedWaivers) != 1 {
		t.Fatalf("核对中应能查到这条被拒绝申请，得到 %d 条", len(rep.RejectedWaivers))
	}
}
