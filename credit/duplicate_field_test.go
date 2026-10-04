package credit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 以下常量是 JSON 源文本中的转义写法（Go 解释字符串里的 \\u 即字面反斜杠+u），
// 两种写法经 JSON 解码后都得到键名 "credit"。
const (
	escapedCreditKey    = "\"\\u0063redit\"" // JSON 解码后为 "credit"，仅首字母转义
	escapedCreditKeyAlt = "\"c\\u0072edit\"" // JSON 解码后为 "credit"，仅第二个字母转义
	escapedAKey         = "\"a\\u0030\""     // JSON 解码后为 "a0"
)

// TestLoadRejectsDuplicateObjectFields 同一 JSON 对象内字段名重复时，无论
// 重复的是学分、学生编号还是其他字段，无论两个值是否相同、是否相邻，整份
// 文件都必须判为内容损坏：Load 报错（错误点名文件与重复字段）、标记文件
// 已存在，且原文件每个字节原样保留。绝不能让后一个值静默顶替前一个值。
func TestLoadRejectsDuplicateObjectFields(t *testing.T) {
	cases := map[string]struct {
		content string
		field   string
	}{
		"课程学分重复且值不同": {
			`{"version":1,"courses":[` +
				`{"id":"c1","name":"数学","credit":4,"credit":9,"open":true}]}` + "\n",
			"credit",
		},
		"课程学分重复但值相同": {
			`{"version":1,"courses":[` +
				`{"id":"c1","name":"数学","credit":4,"credit":4,"open":true}]}` + "\n",
			"credit",
		},
		"学分字段不相邻重复": {
			`{"version":1,"courses":[` +
				`{"credit":4,"id":"c1","name":"数学","open":true,"credit":5}]}` + "\n",
			"credit",
		},
		"修读记录学生编号重复": {
			`{"version":1,"students":[{"id":"s1"},{"id":"s2"}],"enrollments":[` +
				`{"id":"e1","student":"s1","student":"s2","req":"r1",` +
				`"term":"2024春","result":"enrolled"}]}` + "\n",
			"student",
		},
		"要求记录学生编号重复": {
			`{"version":1,"students":[{"id":"s1"}],"courses":[` +
				`{"id":"c1","name":"数学","credit":4,"open":true}],"requirements":[` +
				`{"id":"r1","student":"s1","course":"c1","student":"s1"}]}` + "\n",
			"student",
		},
		"免修记录学生编号重复": {
			`{"version":1,"students":[{"id":"s1"}],"waivers":[` +
				`{"id":"w1","student":"s1","student":"s1","req":"r1",` +
				`"basis":"竞赛获奖","status":"rejected",` +
				`"reason":"目标要求 r1 不存在或不属于该学生"}]}` + "\n",
			"student",
		},
		"顶层courses重复且第二份为空数组": {
			`{"version":1,"courses":[{"id":"c1","name":"数学","credit":4,"open":true}],` +
				`"courses":[]}` + "\n",
			"courses",
		},
		"顶层version重复": {
			`{"version":1,"version":1}` + "\n",
			"version",
		},
		"数组第二条记录内重复": {
			`{"version":1,"students":[{"id":"s1"},{"id":"s2","id":"s3"}]}` + "\n",
			"id",
		},
		"Unicode转义键名与直接书写重复": {
			`{"version":1,"courses":[` +
				`{"id":"c1","name":"数学","credit":4,` + escapedCreditKey + `:9,"open":true}]}` + "\n",
			"credit",
		},
		"两种Unicode写法同名键重复": {
			`{"version":1,"courses":[` +
				`{"id":"c1","name":"数学",` + escapedCreditKey + `:4,` + escapedCreditKeyAlt + `:9,"open":true}]}` + "\n",
			"credit",
		},
		"免修依据字段重复": {
			`{"version":1,"students":[{"id":"s1"}],"waivers":[` +
				`{"id":"w1","student":"s1","req":"r1","basis":"依据甲",` +
				`"basis":"依据乙","status":"rejected","reason":"x"}]}` + "\n",
			"basis",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "records.json")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			s, existed, err := Load(path)
			if err == nil {
				t.Fatalf("重复字段文件必须拒绝读取，却得到 store=%v", s)
			}
			if !existed {
				t.Fatal("损坏文件应标记为已存在")
			}
			msg := err.Error()
			if !strings.Contains(msg, path) {
				t.Fatalf("错误信息应点名所读文件，得到：%v", err)
			}
			if !strings.Contains(msg, "内容损坏") {
				t.Fatalf("错误信息应说明内容损坏，得到：%v", err)
			}
			if !strings.Contains(msg, tc.field) {
				t.Fatalf("错误信息应指出重复字段名 %q，得到：%v", tc.field, err)
			}
			var dupErr *duplicateFieldError
			if errors.As(err, &dupErr) {
				t.Fatalf("Load 返回的错误不应泄露内部扫描错误类型，得到 %T", err)
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != tc.content {
				t.Fatalf("拒绝读取不得改动原文件\nwant=%q\n got=%q", tc.content, got)
			}
		})
	}
}

// TestLoadAllowsSameFieldAcrossDifferentObjects 限制只针对同一个对象：
// 不同课程各有 credit、不同学生名下各自的同号要求/修读/免修，以及字符串
// 内容里提到字段名，都必须照常读取，学分与归属规则保持原样。
func TestLoadAllowsSameFieldAcrossDifferentObjects(t *testing.T) {
	content := `{
  "version": 1,
  "courses": [
    {"id": "c1", "name": "credit 字段简介", "credit": 4, "open": true},
    {"id": "c2", "name": "student 编号说明", "credit": 9, "open": true}
  ],
  "students": [{"id": "s1"}, {"id": "s2"}],
  "requirements": [
    {"id": "r1", "student": "s1", "course": "c1"},
    {"id": "r1", "student": "s2", "course": "c2"}
  ],
  "enrollments": [
    {"id": "e1", "student": "s1", "req": "r1", "term": "2024春",
     "result": "passed", "resultSeq": 1},
    {"id": "e1", "student": "s2", "req": "r1", "term": "2024春",
     "result": "enrolled"}
  ],
  "waivers": [
    {"id": "w1", "student": "s2", "req": "r1", "basis": "材料中提到 student 字段",
     "status": "rejected", "reason": "该要求已有有效免修 w9"}
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
		t.Fatalf("跨对象同名字段应正常读取：existed=%v err=%v", existed, err)
	}
	// 两门课各自的学分不得串行。
	if c := s.Course("c1"); c == nil || c.Credit != 4 {
		t.Fatalf("c1 应为 4 学分，得到 %+v", c)
	}
	if c := s.Course("c2"); c == nil || c.Credit != 9 {
		t.Fatalf("c2 应为 9 学分，得到 %+v", c)
	}
	// 同号修读各归各：s1 通过计 4 学分；s2 仍选课，且 c2 停开不影响读取。
	if rep := s.CheckStudent("s1"); rep.TotalCredits != 4 {
		t.Fatalf("s1 应得 4 学分，得到 %d", rep.TotalCredits)
	}
	if rep := s.CheckStudent("s2"); rep.TotalCredits != 0 || len(rep.Unmet) != 1 {
		t.Fatalf("s2 应为 0 学分且要求未满足，得到 %+v", rep)
	}
	if e := s.Enrollment("s2", "e1"); e == nil || e.Result != Enrolled {
		t.Fatalf("s2 的同号修读应保持选课归属：%+v", e)
	}
	if w := s.Waiver("s2", "w1"); w == nil || w.Status != WaiverRejected ||
		!strings.Contains(w.Basis, "student 字段") {
		t.Fatalf("s2 的免修历史应原样保留（依据文字含字段名也合法）：%+v", w)
	}
}

// TestLoadDuplicateFixedFileWorksNormally 修正重复字段后的合法记录仍可通过
// 原有入口正常使用：以第一份学分 4 为准修复后，核对与后续登记行为不变。
func TestLoadDuplicateFixedFileWorksNormally(t *testing.T) {
	fixed := `{"version":1,"courses":[` +
		`{"id":"c1","name":"数学","credit":4,"open":true}],` +
		`"students":[{"id":"s1"}],` +
		`"requirements":[{"id":"r1","student":"s1","course":"c1"}],` +
		`"enrollments":[{"id":"e1","student":"s1","req":"r1","term":"2024春",` +
		`"result":"passed","resultSeq":1}],"waivers":[],"nextResultSeq":1}` + "\n"
	path := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(path, []byte(fixed), 0o600); err != nil {
		t.Fatal(err)
	}
	s, _, err := Load(path)
	if err != nil {
		t.Fatalf("修正后的文件应可正常读取：%v", err)
	}
	if rep := s.CheckStudent("s1"); rep.TotalCredits != 4 {
		t.Fatalf("修复后应按 4 学分核对，得到 %d", rep.TotalCredits)
	}
	// 既有写入路径仍正常工作。
	if _, _, err := s.AddStudent("s2"); err != nil {
		t.Fatalf("修复后继续登记应正常：%v", err)
	}
}

// TestScanDuplicateKeys 直接覆盖扫描器的边界情形。
func TestScanDuplicateKeys(t *testing.T) {
	okCases := map[string]string{
		"空对象":            `{}`,
		"空数组":            `[]`,
		"无重复对象":          `{"a":1,"b":[1,2,{"c":"x"}]}`,
		"数组各元素同名键":       `[{"a":1},{"a":2}]`,
		"字符串值里出现字段名":     `{"a":"b, b again","b":1}`,
		"嵌套对象内外同名字段":     `{"a":{"a":1},"b":2}`,
		"空字符串键与值":        `{"":"","x":""}`,
		"Unicode转义后仍不同名": `{` + escapedAKey + `:1,"b":1}`,
		"键名含转义引号与控制符":    `{"a\"b":1,"a\b":2}`,
		"对象在数组深层":        `[[[{"a":1}]],{"b":2}]`,
		"标量顶层值":          `42`,
		"记录前后有空白":        "\n\t {\"a\":1} \r\n",
		"首份值之后的重复键不予扫描":  `{"a":1}{"a":1,"a":2}`,
	}
	for name, in := range okCases {
		t.Run("合法/"+name, func(t *testing.T) {
			if err := scanDuplicateKeys(strings.NewReader(in)); err != nil {
				t.Fatalf("不应报重复，得到 %v（输入 %q）", err, in)
			}
		})
	}

	dupCases := map[string]string{
		"对象内直接重复":     `{"a":1,"a":2}`,
		"嵌套对象内重复":     `{"x":{"a":1,"a":2}}`,
		"数组元素对象内重复":   `[{"a":1,"a":2}]`,
		"重复键不相邻":      `{"a":1,"b":2,"c":3,"a":4}`,
		"值相同仍重复":      `{"a":{"x":1},"a":{"x":1}}`,
		"转义键名与直接书写同名": `{"credit":4,` + escapedCreditKey + `:9}`,
		"两种转义写法同名":    `{` + escapedCreditKey + `:4,` + escapedCreditKeyAlt + `:9}`,
		"深层嵌套重复":      `{"x":[{"y":{"z":1,"z":2}}]}`,
		"内外层同时重复必然报错": `{"a":1,"b":{"k":1,"k":2},"a":2}`,
	}
	for name, in := range dupCases {
		t.Run("重复/"+name, func(t *testing.T) {
			err := scanDuplicateKeys(strings.NewReader(in))
			if err == nil {
				t.Fatalf("应报重复字段（输入 %q）", in)
			}
			var dupErr *duplicateFieldError
			if !errors.As(err, &dupErr) || dupErr.field == "" {
				t.Fatalf("应返回带字段名的 duplicateFieldError，得到 %T: %v", err, err)
			}
		})
	}

	// 扫描器只负责发现重复字段：值尚未读完就遇到 EOF 时不报“重复”，
	// 语法残缺由 Load 中随后的普通结构体解码阶段判为损坏。
	if err := scanDuplicateKeys(strings.NewReader(`{"a":`)); err != nil {
		var dupErr *duplicateFieldError
		if errors.As(err, &dupErr) {
			t.Fatalf("残缺 JSON 不应被误报为重复字段，得到 %v", err)
		}
	}
	if err := scanDuplicateKeys(strings.NewReader("")); err != nil {
		t.Fatalf("空输入不在扫描阶段报错（交由调用方），得到 %v", err)
	}
}
