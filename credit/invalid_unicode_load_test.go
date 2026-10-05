package credit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归“记录文件中的字符串必须能还原为合法 Unicode 文字”。
// 标准库 encoding/json 解码时会把非法 UTF-8 字节与未配对的高/低位代理项
// 转义静默替换成 U+FFFD（“�”），替换后的学生编号、课程名称或免修依据绝不
// 能被当成正常内容继续查询、核对或登记——遇到这样的字符串，整份记录文件
// 都必须判为内容损坏，哪怕问题只在一条已拒绝免修的依据里、或在与本次核对
// 无关的课程名称里。检查只读不写：不修补转义、不删字符、不改动申请状态，
// 原文件逐字节保留。合法文字（中文、直接写入的补充平面字符、完整的高低
// 代理项转义对、转义表达的控制字符、用户确实写入的“�”）仍按原规则使用。

// uesc 拼出一个 JSON Unicode 转义序列：uesc("D800") 返回 6 个字符
// （反斜线、u、D、8、0、0）。用函数拼接是为了避免测试源码直接书写转义
// 文本时被编辑器或工具链转换。
func uesc(hex string) string { return string(rune(0x5C)) + "u" + hex }

// unicodeBaseRecord 返回一份结构完整、引用齐全的记录 JSON：学生 s1 的
// 要求 r1 指向 4 学分课程 c1。各替换目标串唯一，便于用例把非法文字注入
// 学生编号、课程名称、免修依据或字段名。
func unicodeBaseRecord() string {
	return `{
  "version": 1,
  "courses": [
    {"id": "c1", "name": "高等数学", "credit": 4, "open": true}
  ],
  "students": [
    {"id": "s1"}
  ],
  "requirements": [
    {"id": "r1", "student": "s1", "course": "c1"}
  ],
  "enrollments": [],
  "waivers": [],
  "nextResultSeq": 0
}
`
}

// writeRawRecord 把原始 JSON 文本（可能含非法 UTF-8 字节）写入临时记录
// 文件，返回路径与原始字节。
func writeRawRecord(t *testing.T, content string) (path string, raw []byte) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "records.json")
	raw = []byte(content)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("写入测试记录失败：%v", err)
	}
	return path, raw
}

// replaceOnce 把 base 中唯一出现的 old 替换为 rep。
func replaceOnce(t *testing.T, base, old, rep string) string {
	t.Helper()
	if strings.Count(base, old) != 1 {
		t.Fatalf("基础记录中 %q 应恰好出现一次", old)
	}
	return strings.Replace(base, old, rep, 1)
}

// assertInvalidUnicode 断言 Load 拒绝含无效 Unicode 文字的记录：报错点名
// 记录文件、说明内容损坏与无效的 Unicode 文字，文件标记为已存在（不能当作
// 空记录），且原文件逐字节保留。
func assertInvalidUnicode(t *testing.T, path string, raw []byte) {
	t.Helper()
	s, existed, err := Load(path)
	if err == nil {
		t.Fatalf("含无效 Unicode 文字的记录应判内容损坏拒绝读取，却得到 store=%v", s)
	}
	if !existed {
		t.Fatalf("损坏文件应标记为已存在（不能当作空记录），existed=%v err=%v",
			existed, err)
	}
	msg := err.Error()
	for _, want := range []string{path, "内容损坏", "无效的 Unicode 文字"} {
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

// TestLoadRejectsUnpairedSurrogateEscapes Unicode 转义中出现未配对的高/低位
// 代理项（高位后没有紧跟低位、孤立低位、两个高位相连等）时，无论出现在学生
// 编号、课程名称、已拒绝免修的依据还是字段名里，整份记录都判内容损坏。
func TestLoadRejectsUnpairedSurrogateEscapes(t *testing.T) {
	hi, lo := uesc("D800"), uesc("DC00")
	cases := map[string]struct{ old, rep string }{
		"学生编号含孤立高位代理项":  {`"id": "s1"`, `"id": "s` + hi + `1"`},
		"学生编号含孤立低位代理项":  {`"id": "s1"`, `"id": "s` + lo + `1"`},
		"两个高位代理项相连":     {`"id": "s1"`, `"id": "s` + hi + hi + `1"`},
		"高位代理项在字符串末尾":   {`"id": "s1"`, `"id": "s` + hi + `"`},
		"高位代理项后直接写普通字符": {`"id": "s1"`, `"id": "s` + hi + `x"`},
		"高位代理项后是普通字符转义": {`"id": "s1"`, `"id": "s` + hi + uesc("0041") + `"`},
		"课程名称含孤立高位代理项":  {`"name": "高等数学"`, `"name": "` + hi + `数学"`},
		"字段名含孤立低位代理项":   {`"students"`, `"stud` + lo + `ents"`},
		"已拒绝免修的依据含孤立代理项": {`"waivers": []`,
			`"waivers": [{"id": "w1", "student": "s1", "req": "r1", ` +
				`"basis": "材料` + hi + `", "status": "rejected", ` +
				`"reason": "免修依据为空"}]`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			content := replaceOnce(t, unicodeBaseRecord(), c.old, c.rep)
			path, raw := writeRawRecord(t, content)
			assertInvalidUnicode(t, path, raw)
		})
	}
}

// TestLoadRejectsInvalidUTF8InStrings 字符串中直接写入的字节不是合法 UTF-8
// 时（孤立非法字节、截断序列、孤立延续字节、过长编码、用 UTF-8 直接编码的
// 代理项），无论出现在字段值还是字段名里，整份记录都判内容损坏。
func TestLoadRejectsInvalidUTF8InStrings(t *testing.T) {
	bad := string([]byte{0xFF})              // 孤立非法字节
	trunc := string([]byte{0xE9})            // 截断的多字节序列
	cont := string([]byte{0x80})             // 孤立延续字节
	overlong := string([]byte{0xC0, 0xAF})   // 过长编码
	surr := string([]byte{0xED, 0xA0, 0x80}) // 用 UTF-8 直接编码的代理项
	cases := map[string]struct{ old, rep string }{
		"学生编号含孤立非法字节":        {`"id": "s1"`, `"id": "s` + bad + `1"`},
		"课程名称含截断序列":          {`"name": "高等数学"`, `"name": "高` + trunc + `数学"`},
		"学生编号含孤立延续字节":        {`"id": "s1"`, `"id": "s` + cont + `1"`},
		"学生编号含过长编码":          {`"id": "s1"`, `"id": "s` + overlong + `1"`},
		"学生编号含 UTF-8 编码的代理项": {`"id": "s1"`, `"id": "s` + surr + `1"`},
		"字段名含非法字节":           {`"students"`, `"stud` + bad + `ents"`},
		"已拒绝免修的依据含非法字节": {`"waivers": []`,
			`"waivers": [{"id": "w1", "student": "s1", "req": "r1", ` +
				`"basis": "材料` + bad + `", "status": "rejected", ` +
				`"reason": "免修依据为空"}]`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			content := replaceOnce(t, unicodeBaseRecord(), c.old, c.rep)
			path, raw := writeRawRecord(t, content)
			assertInvalidUnicode(t, path, raw)
		})
	}
}

// TestLoadRejectsInvalidUnicodeBeforeComparingIDs 两个编号分别含有不同的
// 未配对代理项转义，被标准库替换后会变成同一个“s�”：读取必须先判文件损坏，
// 绝不能拿替换后的文字去判断两个编号是否相同或重复。
func TestLoadRejectsInvalidUnicodeBeforeComparingIDs(t *testing.T) {
	base := replaceOnce(t, unicodeBaseRecord(), `{"id": "s1"}`,
		`{"id": "s`+uesc("D800")+`"}, {"id": "s`+uesc("DC00")+`"}`)
	path, raw := writeRawRecord(t, base)

	s, existed, err := Load(path)
	if err == nil {
		t.Fatalf("含未配对代理项的记录应判内容损坏，却得到 store=%v", s)
	}
	if !existed {
		t.Fatalf("应标记文件已存在，existed=%v err=%v", existed, err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "无效的 Unicode 文字") {
		t.Fatalf("应报告无效的 Unicode 文字，得到：%v", err)
	}
	if strings.Contains(msg, "重复") {
		t.Fatalf("不能用替换后的文字判断编号相同/重复，得到：%v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(raw) {
		t.Fatal("拒绝读取不得改写原文件")
	}
}

// TestLoadAcceptsValidUnicodeText 合法文字仍按原规则正常使用：中文、直接写入
// 的补充平面字符、完整的高低代理项转义对、转义表达的控制字符，以及用户确实
// 写入的“�”（直接写出或转义写出）都不能让文件被判坏。
func TestLoadAcceptsValidUnicodeText(t *testing.T) {
	cases := map[string]struct{ old, rep string }{
		"直接写入的替换字符":   {`{"id": "s1"}`, `{"id": "s1"}, {"id": "s�1"}`},
		"转义写出的替换字符":   {`{"id": "s1"}`, `{"id": "s1"}, {"id": "s` + uesc("FFFD") + `1"}`},
		"完整代理项转义对":    {`"name": "高等数学"`, `"name": "` + uesc("D83D") + uesc("DE00") + `数学"`},
		"直接写入的补充平面字符": {`"name": "高等数学"`, `"name": "😀数学"`},
		"转义表达的控制字符":   {`{"id": "s1"}`, `{"id": "s1"}, {"id": "s` + uesc("0001") + `t"}`},
		"转义表达的空白字符":   {`"name": "高等数学"`, `"name": "高等` + uesc("0020") + `数学"`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			content := replaceOnce(t, unicodeBaseRecord(), c.old, c.rep)
			path, raw := writeRawRecord(t, content)
			s, existed, err := Load(path)
			if err != nil || !existed {
				t.Fatalf("合法文字应正常读取，existed=%v err=%v", existed, err)
			}
			if s == nil {
				t.Fatal("合法记录应返回可用 store")
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != string(raw) {
				t.Fatal("只读访问不得改写原文件")
			}
		})
	}
}

// TestLoadValidUnicodeContentPreserved 合法文字解码后保持原意：完整代理项对
// 还原为对应的补充平面字符，用户写入的“�”按原样保留在编号里，可正常查询。
func TestLoadValidUnicodeContentPreserved(t *testing.T) {
	base := unicodeBaseRecord()
	base = replaceOnce(t, base, `"name": "高等数学"`,
		`"name": "`+uesc("D83D")+uesc("DE00")+`数学"`)
	base = replaceOnce(t, base, `{"id": "s1"}`, `{"id": "s1"}, {"id": "s�2"}`)
	path, raw := writeRawRecord(t, base)

	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("合法记录应正常读取，existed=%v err=%v", existed, err)
	}
	if c := s.Course("c1"); c == nil || c.Name != "😀数学" {
		t.Fatalf("完整代理项对应还原为原字符，得到 %+v", c)
	}
	if st := s.Student("s�2"); st == nil {
		t.Fatal("用户确实写入的“�”是合法文字，学生应能按原编号查到")
	}
	rep := s.CheckStudent("s1")
	if !rep.Found || rep.TotalCredits != 0 || len(rep.Unmet) != 1 {
		t.Fatalf("合法记录应可正常核对，得到 %+v", rep)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(raw) {
		t.Fatal("只读访问不得改写原文件")
	}
}
