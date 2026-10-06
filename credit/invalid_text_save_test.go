package credit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// 本文件回归“保存时拒绝含非法 UTF-8 字节的待保存文字”。
//
// Go 字符串可以携带任意字节：通过登记接口进入内存的编号、名称、学期、依据、
// 原因未必是合法 UTF-8，而 encoding/json 序列化时会把非法字节静默替换成
// U+FFFD（“�”）。若带着这样的文字落盘，免修依据会被改写，两个本来不同的
// 编号还可能被替换成同一个编号，使原本可读的记录文件变成编号重复的损坏
// 文件。因此保存必须在序列化与创建临时文件之前明确拒绝：
//   - 任何一条课程、学生、课程要求、修读、免修记录的任何文字字段非法，都拒
//     绝整次保存，哪怕问题只在一条已拒绝或已撤销免修的文字里；
//   - 错误须说明存在无法原样保存的文字，并指出记录类别与具体字段；
//   - 目标文件不存在时不得留下新记录文件或临时文件，已存在时逐字节保留、
//     原可查询的学生与学分仍可查询；
//   - 待保存记录中的原始字符串不能被修补、清空或替换；
//   - 合法文字（中文、用户输入的“�”、补充平面字符、编号中的控制字符、依据
//     中的空白、字面的反斜线加 uD800）继续正常保存与使用。

// badByte 是一个单独的非法 UTF-8 字节；goodText 里不含它，便于拼接后仍然
// 只有这一处非法。
const badText = "x\xffy"

// saveTextBaseStore 构造一份五类记录齐全的合法记录：一名学生、一门课程、
// 一项要求、一份选课修读，以及一条已拒绝免修（wbad，指向不存在的要求）和
// 一条已撤销免修（w1）。各类别、各历史状态都有记录可被改写注入非法字节。
func saveTextBaseStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "数学", 4)
	mustReq(t, s, "s1", "r1", "c1")
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	if w, _, err := s.ApplyWaiver("s1", "rX", "wbad", "依据材料"); err != nil ||
		w.Status != WaiverRejected {
		t.Fatalf("构造已拒绝免修失败：%+v %v", w, err)
	}
	if _, _, err := s.ApplyWaiver("s1", "r1", "w1", "竞赛获奖"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RevokeWaiver("s1", "w1", "撤销原因"); err != nil {
		t.Fatal(err)
	}
	return s
}

// assertInvalidTextSave 断言 Save 因非法文字失败：返回 *invalidTextError，
// 错误点名记录类别与具体字段并说明无法原样保存；目标文件与任何临时文件都
// 不得出现。
func assertInvalidTextSave(t *testing.T, s *Store, path, category, field string) {
	t.Helper()
	err := s.Save(path)
	if err == nil {
		t.Fatalf("%s.%s 含非法 UTF-8 时应拒绝保存", category, field)
	}
	var ite *invalidTextError
	if !errors.As(err, &ite) {
		t.Fatalf("应返回 *invalidTextError，得到 %T：%v", err, err)
	}
	msg := err.Error()
	for _, want := range []string{"无法原样保存", "不是合法 UTF-8", category, field} {
		if !strings.Contains(msg, want) {
			t.Fatalf("%s.%s 的错误信息应包含 %q，得到：%v", category, field, want, err)
		}
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("%s.%s 拒绝保存后不得留下目标记录文件，stat err=%v", category, field, statErr)
	}
	leftover, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".credit-*.tmp"))
	if len(leftover) != 0 {
		t.Fatalf("%s.%s 拒绝保存后不得留下临时文件：%v", category, field, leftover)
	}
}

// TestSaveRejectsInvalidUTF8EveryCategoryAndField 五类记录的每个文字字段只要
// 含非法 UTF-8 字节，保存都必须失败并指出类别与字段。
func TestSaveRejectsInvalidUTF8EveryCategoryAndField(t *testing.T) {
	cases := []struct {
		name, category, field string
		// mutate 在一份齐全合法记录里把恰好一个字段改成含非法字节的文字。
		mutate func(*Store)
	}{
		{"课程编号", "课程", "id", func(s *Store) { s.courses[0].ID += badText }},
		{"课程名称", "课程", "name", func(s *Store) { s.courses[0].Name += badText }},
		{"学生编号", "学生", "id", func(s *Store) { s.students[0].ID += badText }},
		{"要求编号", "课程要求", "id", func(s *Store) { s.requirements[0].ID += badText }},
		{"要求的学生编号", "课程要求", "student", func(s *Store) { s.requirements[0].StudentID += badText }},
		{"要求的课程编号", "课程要求", "course", func(s *Store) { s.requirements[0].CourseID += badText }},
		{"修读编号", "修读", "id", func(s *Store) { s.enrollments[0].ID += badText }},
		{"修读的学生编号", "修读", "student", func(s *Store) { s.enrollments[0].StudentID += badText }},
		{"修读的要求编号", "修读", "req", func(s *Store) { s.enrollments[0].ReqID += badText }},
		{"修读学期", "修读", "term", func(s *Store) { s.enrollments[0].Term += badText }},
		{"修读结果", "修读", "result", func(s *Store) { s.enrollments[0].Result = Result("passed" + badText) }},
		{"免修编号", "免修", "id", func(s *Store) { s.waivers[0].ID += badText }},
		{"免修的学生编号", "免修", "student", func(s *Store) { s.waivers[0].StudentID += badText }},
		{"免修的要求编号", "免修", "req", func(s *Store) { s.waivers[0].ReqID += badText }},
		{"免修依据", "免修", "basis", func(s *Store) { s.waivers[0].Basis += badText }},
		{"免修状态", "免修", "status", func(s *Store) { s.waivers[0].Status = WaiverStatus("approved" + badText) }},
		{"免修原因", "免修", "reason", func(s *Store) { s.waivers[0].Reason += badText }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "records.json")
			s := saveTextBaseStore(t)
			c.mutate(s)
			assertInvalidTextSave(t, s, path, c.category, c.field)
		})
	}
}

// TestSaveRejectsInvalidTextInRejectedAndRevokedWaivers 即使非法文字只出现在
// 一条已拒绝免修的依据/原因或一条已撤销免修的原依据里，也必须拒绝整次保存：
// 不能删掉那条历史、忽略问题字段或只保存其他记录。
func TestSaveRejectsInvalidTextInRejectedAndRevokedWaivers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.json")
	s := saveTextBaseStore(t)

	var rejectedW, revokedW *Waiver
	for _, w := range s.waivers {
		switch w.Status {
		case WaiverRejected:
			rejectedW = w
		case WaiverRevoked:
			revokedW = w
		}
	}
	if rejectedW == nil || revokedW == nil {
		t.Fatal("准备记录中应同时存在已拒绝与已撤销免修")
	}

	// 已拒绝免修：依据与原因都可能是历史内容。
	rejectedW.Basis = "材料" + badText
	err := s.Save(path)
	if err == nil || !strings.Contains(err.Error(), "免修") ||
		!strings.Contains(err.Error(), "basis") {
		t.Fatalf("已拒绝免修依据含非法字节应拒绝整次保存，得到：%v", err)
	}
	rejectedW.Basis = "依据材料"
	rejectedW.Reason = "目标要求不存在" + badText
	if err := s.Save(path); err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("已拒绝免修原因含非法字节应拒绝整次保存，得到：%v", err)
	}
	rejectedW.Reason = "目标要求 rX 不存在或不属于该学生"

	// 已撤销免修的原依据同样不能被替换着写出去。
	revokedW.Basis = "竞赛获奖" + badText
	if err := s.Save(path); err == nil || !strings.Contains(err.Error(), "basis") {
		t.Fatalf("已撤销免修原依据含非法字节应拒绝整次保存，得到：%v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("全部保存尝试被拒后不得留下记录文件，stat err=%v", statErr)
	}
}

// TestSaveInvalidTextKeepsExistingFileAndQueryable 拒绝保存时已有记录文件必须
// 逐字节保持原样：原先能查询的学生与学分仍可正常查询、读取，本次新增的非法
// 记录不落盘。
func TestSaveInvalidTextKeepsExistingFileAndQueryable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.json")

	// 先保存一份完整合法记录：s1 有一门 4 学分课程、要求、通过修读。
	good := NewStore()
	mustStudent(t, good, "s1")
	mustCourse(t, good, "c1", "数学", 4)
	mustReq(t, good, "s1", "r1", "c1")
	mustEnroll(t, good, "s1", "r1", "2024春", "e1")
	if _, _, err := good.SubmitResult("s1", "e1", Passed); err != nil {
		t.Fatal(err)
	}
	if err := good.Save(path); err != nil {
		t.Fatalf("准备合法记录失败：%v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// 同一份内存记录上再登记一个编号含非法字节的学生，保存必须失败。
	badID := "s" + badText
	if st, _, err := good.AddStudent(badID); err != nil || st.ID != badID {
		t.Fatalf("非法字节编号应原样进入待保存记录，st=%+v err=%v", st, err)
	}
	saveErr := good.Save(path)
	if saveErr == nil {
		t.Fatal("含非法字节的新学生应导致保存失败")
	}
	var ite *invalidTextError
	if !errors.As(saveErr, &ite) || ite.category != "学生" || ite.field != "id" {
		t.Fatalf("应点名学生记录的编号字段，得到：%v", saveErr)
	}

	// 原文件逐字节保留。
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("拒绝保存后原文件应可读：%v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("原文件必须逐字节保留\nwant=%q\n got=%q", raw, got)
	}

	// 重新读取：原学生、学分与修读照常可查，非法记录没有落盘。
	loaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("原文件应仍可正常读取，existed=%v err=%v", existed, err)
	}
	rep := loaded.CheckStudent("s1")
	if !rep.Found || rep.TotalCredits != 4 {
		t.Fatalf("原学生学分应仍可正常核对，得到 %+v", rep)
	}
	if loaded.Student(badID) != nil {
		t.Fatal("保存被拒的非法编号学生不得出现在记录文件中")
	}
}

// TestSaveInvalidTextDoesNotMutateRecords 保存被拒绝后，待保存记录中的原始
// 字符串不能被修补、清空或替换：内存中的非法字节原样保留，仍能按原文字查到
// 该条记录，修正为合法文字后即可正常保存。
func TestSaveInvalidTextDoesNotMutateRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.json")
	s := saveTextBaseStore(t)
	w := s.waivers[0]
	original := "材料" + badText
	w.Basis = original

	if err := s.Save(path); err == nil {
		t.Fatal("含非法依据应拒绝保存")
	}
	if w.Basis != original {
		t.Fatalf("拒绝保存不得修补原始字符串：\nwant=%q\n got=%q", original, w.Basis)
	}
	if utf8.ValidString(w.Basis) {
		t.Fatal("原始非法字节不应被替换成 U+FFFD 等合法文字")
	}
	// 仍按原始（非法）文字作为依据保留在该免修上，没有被清空或删除。
	if got := s.Waiver(w.StudentID, w.ID); got == nil || got.Basis != original {
		t.Fatalf("免修历史及其原依据必须保留，got=%+v", got)
	}
	if len(s.Waivers(w.StudentID)) != 2 {
		t.Fatal("不能为了保存成功删掉有问题的历史记录")
	}

	// 调用方把文字修正为合法 UTF-8 后，同一份记录即可正常保存与读取。
	w.Basis = "修正后的材料"
	if err := s.Save(path); err != nil {
		t.Fatalf("修正为合法文字后应能正常保存：%v", err)
	}
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("正常保存后应可读取：%v", err)
	}
	if got := loaded.Waiver(w.StudentID, w.ID); got == nil || got.Basis != "修正后的材料" {
		t.Fatalf("修正后的依据应原样保存，got=%+v", got)
	}
}

// TestSaveAcceptsValidSpecialText 合法文字继续正常保存与往返使用：用户输入的
// “�”（U+FFFD）、中文与补充平面字符、编号中的控制字符、免修依据中原有空白、
// 以及字面的反斜线加 uD800（只是普通 ASCII 文字，不是文件里的 Unicode 转义）。
func TestSaveAcceptsValidSpecialText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.json")
	s := NewStore()

	// 用户确实输入的“�”是合法字符。
	mustStudent(t, s, "s�1")
	// 补充平面字符出现在课程名称，中文学分正常。
	mustCourse(t, s, "c1", "😀数学", 4)
	// 编号中合法的控制字符按原文保留。
	ctrlID := "s\x01t"
	mustStudent(t, s, ctrlID)
	// 字面文字 \uD800：反斜线与普通字母数字，六个 ASCII 字符。
	literal := `\uD800`
	mustStudent(t, s, literal)

	mustReq(t, s, "s�1", "r1", "c1")
	// 免修依据前后与中间的空白在含实际文字时原样保留（有效免修）。
	basis := "  竞赛\t获奖\n"
	if w, _, err := s.ApplyWaiver("s�1", "r1", "w1", basis); err != nil ||
		w.Status != WaiverApproved {
		t.Fatalf("带空白依据应正常生效：%+v %v", w, err)
	}

	if err := s.Save(path); err != nil {
		t.Fatalf("全部为合法 UTF-8 时保存不应失败：%v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(raw) {
		t.Fatal("合法文字保存后的文件必须全部是合法 UTF-8")
	}
	// 字面 \uD800 按普通文字 JSON 编码（反斜线被转义成 \\uD800），绝不能
	// 写成真正的代理项转义而被读取方判坏。
	if !strings.Contains(string(raw), `\\uD800`) {
		t.Fatalf("字面 \\uD800 应按普通文字保存，content=%q", raw)
	}

	loaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("合法特殊文字保存后应可正常读取：%v", err)
	}
	if loaded.Student("s�1") == nil {
		t.Fatal("含用户输入 U+FFFD 的编号应能按原文查到")
	}
	if c := loaded.Course("c1"); c == nil || c.Name != "😀数学" {
		t.Fatalf("补充平面字符名称应原样保留：%+v", c)
	}
	if loaded.Student(ctrlID) == nil {
		t.Fatal("编号中的控制字符应原样保留并可查询")
	}
	if loaded.Student(literal) == nil {
		t.Fatal("字面 \\uD800 只是普通文字，应按该编号查到学生")
	}
	if w := loaded.Waiver("s�1", "w1"); w == nil || w.Basis != basis {
		t.Fatalf("依据中的原有空白应原样保留：%+v", w)
	}
	rep := loaded.CheckStudent("s�1")
	if !rep.Found || rep.TotalCredits != 4 {
		t.Fatalf("正常保存后学分来源与要求满足情况不应改变，得到 %+v", rep)
	}
}
