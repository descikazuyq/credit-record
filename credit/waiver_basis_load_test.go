package credit

import (
	"os"
	"strings"
	"testing"
)

// 本文件为“读取已有记录时，有效免修的依据校验与正常申请规则一致”提供回归
// 保障。正常申请时空白依据会被拒绝（申请保留为已拒绝），因此带空白依据的
// 有效免修不可能经正常流程落入文件；用例直接构造记录文件，聚焦 Load 路径：
//   - 有效免修的依据为空，或全部由空白字符组成（空格、制表符、换行，以及
//     全角空格 U+3000、不换行空格 U+00A0，含混合使用）：整份记录按内容
//     损坏拒绝读取，错误点名问题文件、所属学生与免修编号，原文件逐字节
//     保留；即使该要求另有通过修读也不能绕过校验；
//   - 依据含有实际文字时，前后或中间带空白（缩进、换行）都正常读取，
//     保存的内容不被修剪或改写；
//   - 已拒绝申请仍允许依据为空或只含空白，不因此判坏文件，也不获得学分。

// blankBasisRecord 构造结构完整、引用齐全的记录：学生 s1 的要求 r1 指向
// 4 学分课程 c1，名下有一份状态为有效、依据为 basis 的免修 w1。
// withPass 时再附一条已通过修读，用于证明通过记录不能绕过依据校验。
func blankBasisRecord(basis string, withPass bool) *fileData {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
		},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
		Waivers: []*Waiver{
			{ID: "w1", StudentID: "s1", ReqID: "r1", Basis: basis, Status: WaiverApproved},
		},
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

// assertBlankBasisRejected 是空白依据用例的共同断言：Load 必须失败、文件
// 标记为已存在，错误点名问题文件、内容损坏、所属学生与免修编号，
// 且原文件字节完整保留。
func assertBlankBasisRejected(t *testing.T, path string, raw []byte) {
	t.Helper()
	s, existed, err := Load(path)
	if err == nil {
		t.Fatalf("有效免修缺少实际依据时必须整份拒绝，却得到 store=%v", s)
	}
	if !existed {
		t.Fatalf("损坏记录应标记为文件已存在，existed=%v err=%v", existed, err)
	}
	msg := err.Error()
	for _, want := range []string{path, "内容损坏", "s1", "w1", "有效免修", "依据"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误信息应包含 %q（文件/学生/免修编号/依据说明），得到：%v", want, err)
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

// TestLoadRejectsApprovedWaiverWithBlankBasis 有效免修的依据为空或只含空白
// 字符（含全角空格、不换行空格及混合空白）时，整份记录按内容损坏拒绝读取。
func TestLoadRejectsApprovedWaiverWithBlankBasis(t *testing.T) {
	bases := map[string]string{
		"空字符串":   "",
		"半角空格":   "   ",
		"制表符与换行": " \t\n\r ",
		"全角空格":   "　　",
		"不换行空格":  "  ",
		"混合各种空白": " \t　\n \r　 ",
	}
	for name, basis := range bases {
		t.Run(name, func(t *testing.T) {
			path, raw := writeRecord(t, blankBasisRecord(basis, false))
			assertBlankBasisRejected(t, path, raw)
		})
	}
}

// TestLoadBlankBasisNotMaskedByPassedEnrollment 即使目标要求另有通过修读
// （本来足以满足要求并解释学分），有效免修缺少实际依据仍是记录损坏：
// 不能拿着通过记录继续核对，整份拒绝且原文件保留。
func TestLoadBlankBasisNotMaskedByPassedEnrollment(t *testing.T) {
	path, raw := writeRecord(t, blankBasisRecord(" 　\n", true))
	assertBlankBasisRejected(t, path, raw)
}

// TestLoadApprovedWaiverWithTextAndWhitespaceLoads 依据含有实际文字时，
// 前后或中间带空白（缩进、换行）都应正常读取：记录可打开、要求由该免修
// 满足，且保存的依据内容原样保留，不被修剪或改写。
func TestLoadApprovedWaiverWithTextAndWhitespaceLoads(t *testing.T) {
	basis := "  课程证明：\n\t外校同层次课程已修毕\n  附成绩单编号 2024-018  "
	d := blankBasisRecord(basis, false)
	path, raw := writeRecord(t, d)

	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("依据含实际文字（带缩进与换行）应正常读取，existed=%v err=%v", existed, err)
	}
	w := s.Waiver("s1", "w1")
	if w == nil || w.Status != WaiverApproved {
		t.Fatalf("免修 w1 应保持有效，得到 %+v", w)
	}
	if w.Basis != basis {
		t.Fatalf("读取不得修剪或改写已保存的依据\nwant=%q\n got=%q", basis, w.Basis)
	}
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
		t.Fatalf("r1 应由有效免修满足并计 4 学分，得到学分=%d 未满足=%v",
			rep.TotalCredits, rep.Unmet)
	}
	if st := rep.Requirements[0]; !st.Satisfied || st.Source != "waiver" || st.WaiverID != "w1" {
		t.Fatalf("来源应是有效免修 w1，得到 %+v", st)
	}
	// 只读核对不得改动文件。
	if s.Dirty() {
		t.Fatal("只读核对不应把记录标记为已变更")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(raw) {
		t.Fatal("只读核对不得改写原文件")
	}
}

// TestLoadRejectedWaiverWithBlankBasisStillLoads 已拒绝的申请仍允许依据为空
// 或只含空白：记录正常打开，拒绝状态与原因照常保留可查，不因此判坏文件，
// 也不获得学分。
func TestLoadRejectedWaiverWithBlankBasisStillLoads(t *testing.T) {
	for name, basis := range map[string]string{
		"空依据":   "",
		"全空白依据": " \t　\n ",
	} {
		t.Run(name, func(t *testing.T) {
			d := blankBasisRecord("", false)
			d.Waivers = []*Waiver{
				{ID: "w1", StudentID: "s1", ReqID: "r1", Basis: basis,
					Status: WaiverRejected, Reason: "免修依据为空"},
			}
			path, _ := writeRecord(t, d)
			s, _, err := Load(path)
			if err != nil {
				t.Fatalf("已拒绝申请的空白依据不应判坏文件：%v", err)
			}
			w := s.Waiver("s1", "w1")
			if w == nil || w.Status != WaiverRejected || w.Reason != "免修依据为空" {
				t.Fatalf("已拒绝申请应保留原状态与原因，得到 %+v", w)
			}
			if w.Basis != basis {
				t.Fatalf("已拒绝申请的依据应原样保留，want=%q got=%q", basis, w.Basis)
			}
			rep := s.CheckStudent("s1")
			if rep.TotalCredits != 0 || len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
				t.Fatalf("已拒绝申请不应获得学分，得到学分=%d 未满足=%v",
					rep.TotalCredits, rep.Unmet)
			}
			if len(rep.RejectedWaivers) != 1 || rep.RejectedWaivers[0].Waiver.ID != "w1" {
				t.Fatalf("核对中应能查到被拒绝申请 w1，得到 %+v", rep.RejectedWaivers)
			}
		})
	}
}
