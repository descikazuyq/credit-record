package credit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// 本文件回归“保存时不得悄悄改写文字”。标准库 encoding/json 序列化会把字符串
// 中的非法 UTF-8 字节静默替换成 U+FFFD（“�”）却返回成功：若照常保存，免修
// 依据会被改掉，两个本来不同的编号还可能被替换成同一个编号，让原本可读的
// 记录文件变成编号重复的损坏文件。因此凡是要写入文件的文字——课程、学生、
// 课程要求、修读、免修的编号、名称、学期、依据、原因等——都必须是合法
// UTF-8；任何一条记录（哪怕是已拒绝或已撤销的免修）的任何一个字段不合法，
// 都必须拒绝整次保存：不修补/清空/替换原始字符串，不删除历史，不创建或
// 改动目标文件。合法文字（用户确实输入的“�”、补充平面字符、编号中合法的
// 控制字符、依据中原有的空白、字面上的“\uD800”）继续按原文保存。

// badByte 是一个单独的非法 UTF-8 字节，放进任何字符串都会让该串非法。
var badByte = string([]byte{0xFF})

// saveTextBaseStore 构造一份结构完整、各条引用齐全的记录，供用例把非法文字
// 注入某个具体字段：课程 c1、学生 s1、要求 r1、修读 e1（选课）、有效免修
// w1 与一条已拒绝免修 wbad。
func saveTextBaseStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore()
	mustCourse(t, s, "c1", "数学", 4)
	mustStudent(t, s, "s1")
	mustReq(t, s, "s1", "r1", "c1")
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	if w, _, err := s.ApplyWaiver("s1", "r1", "w1", "竞赛获奖依据"); err != nil || w.Status != WaiverApproved {
		t.Fatalf("构造有效免修失败：%+v %v", w, err)
	}
	if w, _, err := s.ApplyWaiver("s1", "不存在的要求", "wbad", "旧依据"); err != nil ||
		w.Status != WaiverRejected {
		t.Fatalf("构造被拒绝免修失败：%+v %v", w, err)
	}
	return s
}

// corruptField 在 store 中定位某个要校验的字符串字段并写入非法 UTF-8。
// 返回用于恢复断言的字段定位（类别、字段标签）。
type fieldTarget struct {
	category string
	field    string
	corrupt  func(s *Store)
	// rawField 返回被改字段当前的原始内容，供“未被修补”断言使用。
	rawField func(s *Store) string
}

func saveFieldTargets() []fieldTarget {
	bad := "x" + badByte
	return []fieldTarget{
		{"课程", "编号", func(s *Store) { s.courses[0].ID = bad },
			func(s *Store) string { return s.courses[0].ID }},
		{"课程", "名称", func(s *Store) { s.courses[0].Name = bad },
			func(s *Store) string { return s.courses[0].Name }},
		{"学生", "编号", func(s *Store) { s.students[0].ID = bad },
			func(s *Store) string { return s.students[0].ID }},
		{"课程要求", "编号", func(s *Store) { s.requirements[0].ID = bad },
			func(s *Store) string { return s.requirements[0].ID }},
		{"课程要求", "学生编号", func(s *Store) { s.requirements[0].StudentID = bad },
			func(s *Store) string { return s.requirements[0].StudentID }},
		{"课程要求", "课程编号", func(s *Store) { s.requirements[0].CourseID = bad },
			func(s *Store) string { return s.requirements[0].CourseID }},
		{"修读", "编号", func(s *Store) { s.enrollments[0].ID = bad },
			func(s *Store) string { return s.enrollments[0].ID }},
		{"修读", "学生编号", func(s *Store) { s.enrollments[0].StudentID = bad },
			func(s *Store) string { return s.enrollments[0].StudentID }},
		{"修读", "要求编号", func(s *Store) { s.enrollments[0].ReqID = bad },
			func(s *Store) string { return s.enrollments[0].ReqID }},
		{"修读", "学期", func(s *Store) { s.enrollments[0].Term = bad },
			func(s *Store) string { return s.enrollments[0].Term }},
		{"修读", "结果", func(s *Store) { s.enrollments[0].Result = Result(bad) },
			func(s *Store) string { return string(s.enrollments[0].Result) }},
		{"免修", "编号", func(s *Store) { s.waivers[0].ID = bad },
			func(s *Store) string { return s.waivers[0].ID }},
		{"免修", "学生编号", func(s *Store) { s.waivers[0].StudentID = bad },
			func(s *Store) string { return s.waivers[0].StudentID }},
		{"免修", "要求编号", func(s *Store) { s.waivers[0].ReqID = bad },
			func(s *Store) string { return s.waivers[0].ReqID }},
		{"免修", "依据", func(s *Store) { s.waivers[0].Basis = bad },
			func(s *Store) string { return s.waivers[0].Basis }},
		{"免修", "状态", func(s *Store) { s.waivers[0].Status = WaiverStatus(bad) },
			func(s *Store) string { return string(s.waivers[0].Status) }},
		{"免修", "原因", func(s *Store) { s.waivers[0].Reason = bad },
			func(s *Store) string { return s.waivers[0].Reason }},
	}
}

// TestSaveRejectsInvalidUTF8PerField 待保存记录里任意字符串字段含非法
// UTF-8 时，Save 必须在写入前拒绝：错误说明存在无法原样保存的文字，并
// 指出记录类别与具体字段；目标文件（原本不存在）不得被创建；原始字符串
// 不能被修补、清空或替换。
func TestSaveRejectsInvalidUTF8PerField(t *testing.T) {
	for _, tg := range saveFieldTargets() {
		t.Run(tg.category+"-"+tg.field, func(t *testing.T) {
			s := saveTextBaseStore(t)
			tg.corrupt(s)

			path := filepath.Join(t.TempDir(), "records.json")
			err := s.Save(path)
			if err == nil {
				t.Fatalf("%s的%s含非法 UTF-8 时应拒绝保存", tg.category, tg.field)
			}
			msg := err.Error()
			for _, want := range []string{"无法原样保存", tg.category, tg.field, path} {
				if !strings.Contains(msg, want) {
					t.Fatalf("错误应说明无法原样保存并指出类别 %q 与字段 %q、点名目标文件，得到：%v",
						tg.category, tg.field, err)
				}
			}
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Fatalf("拒绝保存后不得在原本不存在的位置留下记录文件，stat err=%v", statErr)
			}
			// 临时文件也不得残留。
			leftover, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".credit-*.tmp"))
			if len(leftover) != 0 {
				t.Fatalf("拒绝保存后不得残留临时文件：%v", leftover)
			}
			// 原始字符串保持原样：非法字节仍在，绝不能被替换成 U+FFFD。
			raw := tg.rawField(s)
			if utf8.ValidString(raw) || !strings.ContainsRune(raw, 'x') ||
				!strings.Contains(raw, badByte) {
				t.Fatalf("拒绝保存不得修补原始字段，得到 % x", []byte(raw))
			}
			if strings.Contains(raw, "�") {
				t.Fatalf("原始字段不得被替换成 U+FFFD，得到 % x", []byte(raw))
			}
		})
	}
}

// TestSaveRejectedRevokedWaiverInvalidTextRefusesWholeSave 即使非法文字只
// 出现在一条已拒绝或已撤销免修的字段里，也必须拒绝整次保存，不能删掉那条
// 历史、忽略有问题的字段，或只保存其他记录。
func TestSaveRejectedRevokedWaiverInvalidTextRefusesWholeSave(t *testing.T) {
	t.Run("已拒绝免修依据非法", func(t *testing.T) {
		s := saveTextBaseStore(t)
		// 找到被拒绝的 wbad 并污染其依据。
		w := s.Waiver("s1", "wbad")
		if w == nil || w.Status != WaiverRejected {
			t.Fatalf("测试前置：wbad 应为已拒绝免修，得到 %+v", w)
		}
		w.Basis = "旧依据" + badByte
		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err == nil {
			t.Fatal("已拒绝免修依据含非法文字时也必须拒绝整次保存")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("拒绝保存后不得创建记录文件")
		}
		// 历史记录仍在 store 中，未被删除或清空。
		if got := s.Waiver("s1", "wbad"); got == nil || !strings.Contains(got.Basis, badByte) {
			t.Fatalf("拒绝保存不得删除或修补那条已拒绝历史，得到 %+v", got)
		}
	})

	t.Run("已撤销免修原因非法", func(t *testing.T) {
		s := saveTextBaseStore(t)
		if _, _, err := s.RevokeWaiver("s1", "w1", "撤销原因"+badByte); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		if err == nil || !strings.Contains(err.Error(), "免修") ||
			!strings.Contains(err.Error(), "原因") {
			t.Fatalf("已撤销免修原因含非法文字应拒绝整次保存并指出免修/原因，得到：%v", err)
		}
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatal("拒绝保存后不得创建记录文件")
		}
	})
}

// TestSaveInvalidTextKeepsExistingFileUntouched 目标文件已存在且内容完好时，
// 含非法文字的保存必须失败，且原文件逐字节保留；恢复后原先能查到的学生与
// 学分仍可正常查询。
func TestSaveInvalidTextKeepsExistingFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "records.json")

	good := saveTextBaseStore(t)
	if err := good.Save(path); err != nil {
		t.Fatalf("前置正常保存失败：%v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// 再构造一份含非法课程名称的记录，尝试覆盖同一路径。
	bad := saveTextBaseStore(t)
	bad.courses[0].Name = "数学" + badByte
	if err := bad.Save(path); err == nil {
		t.Fatal("含非法文字时应拒绝覆盖已有文件")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("原文件应继续可读：%v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("拒绝保存后原文件必须逐字节保留\nwant=%q\n got=%q", raw, got)
	}

	// 原文件仍可正常读取与核对。
	loaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("原文件应仍可正常读取：existed=%v err=%v", existed, err)
	}
	if loaded.Student("s1") == nil {
		t.Fatal("原先能查询的学生应仍可查询")
	}
	if c := loaded.Course("c1"); c == nil || c.Name != "数学" {
		t.Fatalf("原先的课程名称应保持不变：%+v", c)
	}
	if w := loaded.Waiver("s1", "w1"); w == nil || w.Status != WaiverApproved {
		t.Fatalf("有效免修历史应保持不变：%+v", w)
	}
}

// TestSaveSubmittedResultInvalidText 已有修读记录的结果（通过、未通过）被调用
// 方设为含非法 UTF-8 字节的文字后，再请求保存整份记录，必须在写入前拒绝——
// 非法字节夹在较长文字中间也一样。拒绝后不得创建记录文件或临时文件，内存中的
// 原始结果、结果序号与其他字段都不被替换、清空或修补。
func TestSaveSubmittedResultInvalidText(t *testing.T) {
	for _, good := range []Result{Passed, Failed} {
		t.Run(string(good), func(t *testing.T) {
			s := saveTextBaseStore(t)
			if _, _, err := s.SubmitResult("s1", "e1", good); err != nil {
				t.Fatalf("前置提交结果失败：%v", err)
			}
			e := s.Enrollment("s1", "e1")
			seqBefore := e.ResultSeq
			// 非法字节夹在较长文字中，而不是单独一个字节。
			e.Result = Result("结果前缀-" + string(good) + badByte + "-结果后缀")

			path := filepath.Join(t.TempDir(), "records.json")
			err := s.Save(path)
			if err == nil {
				t.Fatalf("已提交修读的结果含非法 UTF-8（%s）时应拒绝保存", good)
			}
			msg := err.Error()
			for _, want := range []string{"无法原样保存", "修读", "e1", "结果", path} {
				if !strings.Contains(msg, want) {
					t.Fatalf("错误应点名目标文件、修读记录编号 e1 与字段“结果”，得到：%v", err)
				}
			}
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Fatalf("拒绝保存后不得创建记录文件，stat err=%v", statErr)
			}
			leftover, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".credit-*.tmp"))
			if len(leftover) != 0 {
				t.Fatalf("拒绝保存后不得残留临时文件：%v", leftover)
			}

			// 内存中的原始结果与其他字段都不被替换、清空或修补。
			got := s.Enrollment("s1", "e1")
			if got == nil {
				t.Fatal("拒绝保存不得删除原修读记录")
			}
			if !strings.Contains(string(got.Result), badByte) ||
				strings.Contains(string(got.Result), "�") {
				t.Fatalf("原始结果不得被替换或修补，得到 % x", []byte(got.Result))
			}
			if got.ResultSeq != seqBefore {
				t.Fatalf("结果序号不得被改动：before=%d after=%d", seqBefore, got.ResultSeq)
			}
			if got.Term != "2024春" || got.ReqID != "r1" || got.StudentID != "s1" {
				t.Fatalf("修读的其他字段不得被改动：%+v", got)
			}
			// 其他记录全部正常，历史条目不能被删除。
			if s.Waiver("s1", "w1") == nil || s.Waiver("s1", "wbad") == nil {
				t.Fatal("拒绝保存不得删除任何免修历史")
			}
		})
	}
}

// TestSaveRejectedRevokedWaiverStatusInvalidText 已拒绝、已撤销免修自身的状态
// 字段被设为含非法字节的文字时，即使问题免修已经拒绝或已经撤销、不提供学分，
// 或该要求另有通过修读照常满足，也不能跳过它：整次保存必须被拒绝，且不只写入
// 合法部分。错误须点名目标文件、免修类别、原记录编号与“状态”字段。
func TestSaveRejectedRevokedWaiverStatusInvalidText(t *testing.T) {
	// 已有记录文件逐字节保持原样：先落一份完好文件，再用“已拒绝免修状态非法”
	// 的记录集覆盖同一路径，必须失败且原文件一字不改、仍可正常读取。
	t.Run("已拒绝免修状态非法且已有文件保持原样", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "records.json")
		good := saveTextBaseStore(t)
		if err := good.Save(path); err != nil {
			t.Fatalf("前置正常保存失败：%v", err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		bad := saveTextBaseStore(t)
		w := bad.Waiver("s1", "wbad") // 已拒绝、不提供学分的历史
		if w == nil || w.Status != WaiverRejected {
			t.Fatalf("测试前置：wbad 应为已拒绝免修，得到 %+v", w)
		}
		reasonBefore := w.Reason
		w.Status = WaiverStatus("rejected" + badByte + "-tail") // 非法字节夹在较长文字中

		saveErr := bad.Save(path)
		if saveErr == nil {
			t.Fatal("已拒绝免修自身的状态含非法字节时也必须拒绝整次保存")
		}
		msg := saveErr.Error()
		for _, want := range []string{"无法原样保存", "免修", "wbad", "状态", path} {
			if !strings.Contains(msg, want) {
				t.Fatalf("错误应点名目标文件、免修记录编号 wbad 与字段“状态”，得到：%v", saveErr)
			}
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(raw) {
			t.Fatalf("拒绝保存后已有文件必须逐字节保持原样\nwant=%q\n got=%q", raw, got)
		}
		leftover, _ := filepath.Glob(filepath.Join(dir, ".credit-*.tmp"))
		if len(leftover) != 0 {
			t.Fatalf("拒绝保存后不得残留临时文件：%v", leftover)
		}
		// 原文件仍可正常读取，已拒绝历史保持合法的 rejected 状态与原因。
		loaded, existed, err := Load(path)
		if err != nil || !existed {
			t.Fatalf("原文件应仍可正常读取：existed=%v err=%v", existed, err)
		}
		lw := loaded.Waiver("s1", "wbad")
		if lw == nil || lw.Status != WaiverRejected || lw.Reason != reasonBefore {
			t.Fatalf("原文件中的已拒绝免修应保持不变：%+v", lw)
		}
		// 内存中的问题记录不被修补、清空，历史条目也不被删除。
		mw := bad.Waiver("s1", "wbad")
		if mw == nil || !strings.Contains(string(mw.Status), badByte) ||
			strings.Contains(string(mw.Status), "�") {
			t.Fatalf("拒绝保存不得替换或修补内存中的原始状态：%+v", mw)
		}
		if mw.Reason != reasonBefore {
			t.Fatalf("拒绝原因不得被改动：%+v", mw)
		}
	})

	t.Run("已撤销免修状态非法且该要求另有通过修读", func(t *testing.T) {
		s := saveTextBaseStore(t)
		// 先让 r1 拥有通过修读，再撤销 w1：撤销后的免修不提供学分，要求靠
		// 通过修读照常满足——即使这样，问题免修自身的状态非法仍须拒绝整次保存。
		if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.RevokeWaiver("s1", "w1", "材料无法核实"); err != nil {
			t.Fatal(err)
		}
		w := s.Waiver("s1", "w1")
		if w.Status != WaiverRevoked {
			t.Fatalf("测试前置：w1 应为已撤销免修，得到 %+v", w)
		}
		basisBefore := w.Basis
		w.Status = WaiverStatus("revoked" + badByte)

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		if err == nil {
			t.Fatal("已撤销免修自身的状态含非法字节时必须拒绝整次保存，即使该要求另有通过修读")
		}
		msg := err.Error()
		for _, want := range []string{"无法原样保存", "免修", "w1", "状态", path} {
			if !strings.Contains(msg, want) {
				t.Fatalf("错误应点名目标文件、免修记录编号 w1 与字段“状态”，得到：%v", err)
			}
		}
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("拒绝保存后不得创建记录文件，stat err=%v", statErr)
		}
		// 内存中原依据、撤销原因与通过修读都不被改动，历史不被删除。
		mw := s.Waiver("s1", "w1")
		if mw == nil || !strings.Contains(string(mw.Status), badByte) {
			t.Fatalf("内存中的原始状态不得被修补：%+v", mw)
		}
		if mw.Basis != basisBefore || mw.Reason != "材料无法核实" {
			t.Fatalf("原依据与撤销原因不得被改动：%+v", mw)
		}
		if e := s.Enrollment("s1", "e1"); e == nil || e.Result != Passed {
			t.Fatalf("通过修读不得被改动：%+v", e)
		}
	})
}

// TestSaveResultStatusLegalReplacementCharAccepted 修读“结果”与免修“状态”
// 这两个字段与编号、名称、学期、依据、原因遵守同一条 UTF-8 规则：用户确实输入
// 的“�”（U+FFFD）是合法字符，不能仅凭它出现在结果或状态文字中就判为非法
// 字节；只有真正的非法字节才拒绝。这里直接校验这两个字段的判定，避免与“结果/
// 状态只能取固定业务值”的既有读取规则（loadData 的枚举校验）混在一起——业务
// 允许的标准状态（通过/未通过、有效/已拒绝/已撤销）照常保存与读取由其余用例
// 覆盖。
func TestSaveResultStatusLegalReplacementCharAccepted(t *testing.T) {
	for _, c := range []struct {
		category, id, field, value string
	}{
		{"修读", "e1", "结果", "passed�-合法"},
		{"修读", "e1", "结果", "未通过�"},
		{"免修", "w1", "状态", "rejected�-状态"},
		{"免修", "w1", "状态", "�"},
	} {
		if err := checkSaveText(c.category, c.id, c.field, c.value); err != nil {
			t.Fatalf("%s记录 %s 的字段“%s”含合法 U+FFFD 时不应被判为非法文字：%v",
				c.category, c.id, c.field, err)
		}
		// 同一文字中夹入真正的非法字节仍必须拒绝。
		bad := c.value + badByte
		err := checkSaveText(c.category, c.id, c.field, bad)
		if err == nil {
			t.Fatalf("%s记录 %s 的字段“%s”加入非法字节后必须拒绝", c.category, c.id, c.field)
		}
		var ite *invalidTextError
		if !errors.As(err, &ite) || ite.category != c.category || ite.field != c.field || ite.id != c.id {
			t.Fatalf("错误应点名类别 %q、编号 %q 与字段 %q，得到：%v",
				c.category, c.id, c.field, err)
		}
	}
}

// TestSaveDoesNotCollapseDistinctInvalidIDs 两个含有不同非法字节、本不相同
// 的编号，标准库替换后都会变成同一个“x�”：保存必须在替换发生前拒绝，绝不
// 能把它们写成两个同号学生、制造编号重复的损坏文件。
func TestSaveDoesNotCollapseDistinctInvalidIDs(t *testing.T) {
	s := NewStore()
	if _, _, err := s.AddStudent("s" + string([]byte{0xFE})); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddStudent("s" + string([]byte{0x80})); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path); err == nil {
		t.Fatal("含不同非法字节的编号必须拒绝保存，不能写成重复编号")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("拒绝后不得创建文件")
	}
}

// TestSaveAcceptsLegalUnicodeText 合法文字继续按原文保存并可原样读回：
// 用户确实输入的“�”、中文与补充平面字符、编号中合法的控制字符、免修依据
// 中原有（含前后）的空白、命令行字面上的“\uD800”（反斜线加普通字母数字，
// 不是 Unicode 转义）都不能被误拒绝。
func TestSaveAcceptsLegalUnicodeText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "records.json")

	s := NewStore()
	// 编号中合法的控制字符（U+0001）与用户确实输入的“�”。
	mustStudent(t, s, "s\u0001")
	mustStudent(t, s, "s�2")
	// 中文与补充平面字符课程名称。
	mustCourse(t, s, "c1", "高等数学😀", 4)
	mustReq(t, s, "s\u0001", "r1", "c1")
	// 学期含补充平面字符、修读编号含“�”。
	mustEnroll(t, s, "s\u0001", "r1", "2024😀春", "e�1")
	if _, _, err := s.SubmitResult("s\u0001", "e�1", Passed); err != nil {
		t.Fatal(err)
	}
	// 依据前后与中间的空白必须原样保留；命令行里字面出现的“\uD800”只是
	// 反斜线加普通字母数字（合法 ASCII 文字），不是记录文件里的 Unicode 转义。
	literalBackslashU := `\uD800`
	basis := "  竞赛\n获奖\t" + literalBackslashU + "  "
	if w, _, err := s.ApplyWaiver("s\u0001", "r1", "w1", basis); err != nil ||
		w.Status != WaiverApproved {
		t.Fatalf("含空白与字面 \\uD800 的依据应正常生效：%+v %v", w, err)
	}

	if err := s.Save(path); err != nil {
		t.Fatalf("合法文字应正常保存：%v", err)
	}

	loaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("合法文字保存后应可正常读取：%v", err)
	}
	if loaded.Student("s\u0001") == nil {
		t.Fatal("编号中合法的控制字符应原样保存")
	}
	if loaded.Student("s�2") == nil {
		t.Fatal("用户确实输入的“�”是合法文字，应原样保存")
	}
	if c := loaded.Course("c1"); c == nil || c.Name != "高等数学😀" {
		t.Fatalf("中文与补充平面字符应原样保存，得到 %+v", c)
	}
	if e := loaded.Enrollment("s\u0001", "e�1"); e == nil || e.Term != "2024😀春" {
		t.Fatalf("含补充平面字符的学期与含“�”的修读编号应原样保存，得到 %+v", e)
	}
	w := loaded.Waiver("s\u0001", "w1")
	if w == nil || w.Basis != basis || !strings.Contains(w.Basis, literalBackslashU) {
		t.Fatalf("依据中的原有空白与字面 \\uD800 应原样保留，得到 %+v", w)
	}
	// 学分来源与核对结果不改变。
	if rep := loaded.CheckStudent("s\u0001"); rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
		t.Fatalf("正常保存后核对结果不应改变，得到 %+v", rep)
	}
}
