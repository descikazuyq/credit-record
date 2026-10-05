package credit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归“记录文件中的 JSON 字符串必须能按原始内容还原为合法 Unicode
// 文字”：直接写入的非法 UTF-8 字节与未配对的代理项转义不能被标准库替换成
// U+FFFD 后继续使用；问题无论出在字段名还是字段值、出在哪条记录，整份
// 文件都判为内容损坏，且原文件逐字节保留。用户真正写入的 U+FFFD、直接
// 写出的补充平面字符与完整高低代理项对仍按合法文字正常读取。

func TestScanValidUnicodeAcceptsLegal(t *testing.T) {
	cases := map[string]string{
		"普通中文":          `{"id":"学生","name":"高等数学"}`,
		"直接写入的补充平面字符":   `{"id":"😀","name":"𠀀𩰲"}`,
		"完整高低代理项转义对":    `{"id":"a𐀀b"}`,
		"非BMP中文字符代理项对":  `{"name":"𠀀"}`,
		"用户写入的U+FFFD转义": `{"id":"a�b"}`,
		"用户直接写出的替换字符":   "{\"id\":\"a�b\"}",
		"转义控制字符":        `{"basis":"a\tb\nc\rd\u0000e"}`,
		"其他常见转义":        `{"a":"\\\/\"\b\f\r\n\t"}`,
		"空字符串与空输入":      `{"":"","x":[]}`,
		"深层嵌套":          `{"a":[{"b":[{"c":"中文😀𐀀"}]}]}`,
		"记录前后空白":        "\n\t " + `{"id":"s"}` + "\r\n",
		"顶层字符串":         `"s1"`,
		"顶层数字":          `42`,
		"首份值之后的非法字节不扫描": `{"id":"s1"}{"id":"xff"}`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if err := scanValidUnicode([]byte(in)); err != nil {
				t.Fatalf("合法文字不应被拒绝：%v（输入 %q）", err, in)
			}
		})
	}
}

func TestScanValidUnicodeRejectsInvalid(t *testing.T) {
	cases := map[string]string{
		"值中直接写入非法UTF8字节":   `{"id":"s` + "\xff" + `"}`,
		"字段名中直接写入非法UTF8字节": `{"s` + "\xff" + `":1}`,
		"课程名称含截断的三字节序列":    `{"name":"数` + "\xe6\x95" + `"}`,
		"依据中出现孤立续接字节":      `{"basis":"a` + "\x80" + `b"}`,
		"孤立高位代理项":          `{"id":"a\uD800b"}`,
		"孤立低位代理项":          `{"id":"a\uDC00b"}`,
		"两个高位代理项相连":        `{"id":"a\uD800\uD800b"}`,
		"高位代理项后跟普通BMP转义":   `{"id":"a\uD800Ab"}`,
		"高位代理项后字符串结束":      `{"id":"a\uD800"}`,
		"低位代理项位于字段名":       `{"a\uDC00":1}`,
		"高位代理项大写写法":        `{"id":"\uDABC"}`,
		"代理项对跨在两个不同字符串":    `{"a":"\uD800","b":"\uDC00"}`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if err := scanValidUnicode([]byte(in)); err == nil {
				t.Fatalf("无效 Unicode 文字必须报错（输入 %q）", in)
			}
		})
	}
}

// 残缺或语法本身就不合法的文件不应在 Unicode 扫描阶段被误判（也不应被
// 放过）：扫描直接结束，由随后的标准 JSON 解析按语法损坏拒绝。
func TestScanValidUnicodeLeavesSyntaxErrorsToParser(t *testing.T) {
	cases := map[string]string{
		"字符串未结束":    `{"id":"s1`,
		"转义残缺":      `{"id":"s1\uD80"}`,
		"非法转义":      `{"id":"a\x"}`,
		"裸控制字符":     "{\"id\":\"a\x01b\"}",
		"对象缺右括号":    `{"id":"s1"`,
		"高位转义后跟半个u": `{"id":"a\uD800\u"}`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if err := scanValidUnicode([]byte(in)); err != nil {
				t.Fatalf("语法问题不应由 Unicode 扫描报错，得到 %v（输入 %q）", err, in)
			}
		})
	}
}

// TestLoadRejectsInvalidUnicode 端到端覆盖 Load：各种无效 Unicode 情形都
// 必须让整份记录判为内容损坏——错误点名文件、说明存在无效的 Unicode 文字、
// 标记文件已存在，且原文件每个字节原样保留，不返回任何可用于业务的 store。
func TestLoadRejectsInvalidUnicode(t *testing.T) {
	wrap := func(inner string) string {
		return `{"version":1,` + inner + `}` + "\n"
	}
	cases := map[string]string{
		"学生编号含非法字节": wrap(`"students":[{"id":"s1` + "\xff" + `"}]`),
		"课程名称含非法字节": wrap(`"courses":[{"id":"c1","name":"数` + "\xe6" + `","credit":4,"open":true}]`),
		"有效免修依据含非法字节": wrap(
			`"students":[{"id":"s1"}],` +
				`"courses":[{"id":"c1","name":"数学","credit":4,"open":true}],` +
				`"requirements":[{"id":"r1","student":"s1","course":"c1"}],` +
				`"waivers":[{"id":"w1","student":"s1","req":"r1","basis":"获奖` + "\xff" + `","status":"approved"}]`),
		"已拒绝免修依据含孤立代理项": wrap(
			`"students":[{"id":"s1"}],` +
				`"waivers":[{"id":"w1","student":"s1","req":"r9","basis":"\uD800","status":"rejected","reason":"x"}]`),
		"未涉及课程名称含孤立低位代理项": wrap(
			`"students":[{"id":"s1"}],` +
				`"courses":[{"id":"c9","name":"\uDC00","credit":4,"open":true}]`),
		"字段名含孤立代理项":   wrap(`"students":[{"id":"s1","\uD800":"x"}]`),
		"编号含两个高位代理项":  wrap(`"students":[{"id":"s\uD800\uD800"}]`),
		"高位代理项后跟普通字符": wrap(`"students":[{"id":"\uD800A"}]`),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "records.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			s, existed, err := Load(path)
			if err == nil {
				t.Fatalf("含无效 Unicode 的记录必须拒绝读取，却得到 store=%v", s)
			}
			if !existed {
				t.Fatal("损坏文件应标记为已存在")
			}
			msg := err.Error()
			for _, want := range []string{path, "内容损坏", "无效的 Unicode"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("错误信息应包含 %q，得到：%v", want, err)
				}
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != content {
				t.Fatalf("拒绝读取不得改动原文件\nwant=%q\n got=%q", content, got)
			}
		})
	}
}

// TestLoadAcceptsLegalUnicode 合法文字保持原有兼容行为：中文、直接写入的
// 补充平面字符、完整代理项转义对、用户自己写入的 U+FFFD 与转义控制字符都
// 按原意读取，编号归属、依据原文与核对结果不受影响。
func TestLoadAcceptsLegalUnicode(t *testing.T) {
	content := `{
  "version": 1,
  "courses": [
    {"id": "c1", "name": "高等数学😀", "credit": 4, "open": true},
    {"id": "c2", "name": "辅修课𐀀", "credit": 3, "open": true}
  ],
  "students": [
    {"id": "s1"},
    {"id": "s2�"}
  ],
  "requirements": [
    {"id": "r1", "student": "s1", "course": "c1"},
    {"id": "r2", "student": "s1", "course": "c2"}
  ],
  "enrollments": [
    {"id": "e1", "student": "s1", "req": "r1", "term": "2024春\t上",
     "result": "passed", "resultSeq": 1}
  ],
  "waivers": [
    {"id": "w1", "student": "s1", "req": "r2", "basis": "竞赛 获奖\u0000材料", "status": "approved"}
  ],
  "nextResultSeq": 1
}
`
	path := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("合法 Unicode 记录应正常读取：existed=%v err=%v", existed, err)
	}
	if c := s.Course("c2"); c == nil || c.Name != "辅修课𐀀" {
		t.Fatalf("完整代理项对应按补充平面字符读取，得到 %+v", c)
	}
	if st := s.Student("s2�"); st == nil {
		t.Fatal("用户自己写入的 U+FFFD 是合法编号，必须照常读取")
	}
	if s.Student("s2") != nil {
		t.Fatal("含 U+FFFD 的编号不得与其他编号混淆")
	}
	w := s.Waiver("s1", "w1")
	if w == nil || w.Basis != "竞赛 获奖\x00材料" {
		t.Fatalf("转义控制字符与依据原文应原样保留，得到 %+v", w)
	}
	if rep := s.CheckStudent("s1"); rep.TotalCredits != 7 {
		t.Fatalf("合法记录核对结果应保持原样（7 学分），得到 %+v", rep)
	}
}

// TestLoadInvalidUnicodeNotMistakenForEmpty 含无效 Unicode 的文件绝不能被
// 当作空记录：Load 必须返回错误且 existed=true，调用方因此不会覆盖写回。
func TestLoadInvalidUnicodeNotMistakenForEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.json")
	content := `{"students":[{"id":"s1\xff"}]}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s, existed, err := Load(path)
	if err == nil || s != nil {
		t.Fatalf("无效 Unicode 文件不能读出 store：s=%v err=%v", s, err)
	}
	if !existed {
		t.Fatal("必须报告文件已存在，不能当作不存在的空记录")
	}
}
