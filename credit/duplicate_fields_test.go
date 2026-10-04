package credit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDuplicateObjectNameScan 直接验证重复字段扫描器：同一对象内任意字段
// （学分、学生编号、顶层字段等）重复都要识别，字段是否相邻、两值是否相同
// 不影响结果；键名以 JSON 解码后的文字为准（\u 转义同名也算重复）；
// 字段“值”里提到字段名不算重复；不同对象各自的同名字段互不影响。
func TestDuplicateObjectNameScan(t *testing.T) {
	dupCases := map[string]string{
		"课程学分重复(4被9覆盖)": `{"id":"c1","name":"数学","credit":4,"open":true,"credit":9}`,
		"学分重复且两值相同":     `{"id":"c1","name":"数学","credit":4,"credit":4}`,
		"重复字段相邻":        `{"credit":4,"credit":9}`,
		"重复字段间有空白与换行": `{
			"id": "c1",
			"credit": 4,
			"name": "数学",
			"credit": 9
		}`,
		"修读记录学生字段重复": `{"id":"e1","student":"s1","req":"r1","term":"2024春",` +
			`"result":"enrolled","student":"s2"}`,
		"免修记录学生字段重复": `{"id":"w1","student":"s1","req":"r1","basis":"依据",` +
			`"status":"approved","student":"s1"}`,
		"顶层courses重复第二份为空数组": `{"version":1,"courses":[{"id":"c1"}],"courses":[]}`,
		"顶层version重复":        `{"version":1,"version":1}`,
		// 文件字节里第二个键是 \u0063redit（JSON 解码后即 credit）。
		"Unicode转义与直写同名": "{\"credit\":4,\"\\u0063redit\":9}\n",
		// 大写 \u 转义形式同样要先解码再比较（\u0043 = C）。
		"Unicode大写转义与直写同名": "{\"\\u0043REDIT\":4,\"CREDIT\":9}",
		// 两个键都用转义书写，解码后同名也算重复。
		"两个Unicode转义键同名": "{\"\\u0063redit\":4,\"\\u0063redit\":9}",
		"嵌套对象内重复":        `{"version":1,"courses":[{"id":"c1","credit":4,"credit":9}]}`,
	}
	for name, content := range dupCases {
		t.Run(name, func(t *testing.T) {
			got, ok := duplicateObjectName(strings.NewReader(content))
			if !ok {
				t.Fatalf("%s：应识别出重复字段，却未发现\n%s", name, content)
			}
			if got == "" {
				t.Fatalf("%s：应返回重复字段名，得到空串", name)
			}
		})
	}

	okCases := map[string]string{
		"两门课程各有credit": `{"courses":[` +
			`{"id":"c1","credit":4},{"id":"c2","credit":3}]}` + "\n",
		"两条修读各有student": `{"enrollments":[` +
			`{"student":"s1","id":"e1"},{"student":"s2","id":"e2"}]}` + "\n",
		"课程名称的文字内容提到字段名":    `{"id":"c1","name":"credit 字段说明","credit":4,"open":true}`,
		"免修依据文字提到字段名":       `{"id":"w1","student":"s1","basis":"student 编号见附件","status":"approved"}`,
		"Unicode转义后与其他键不同名": "{\"\\u0063reditx\":4,\"credit\":9}",
		"仅数组重复无所谓":          `{"a":[1,1,1],"b":[2,2]}`,
		"空对象空数组":            `{"a":{},"b":[]}`,
	}
	for name, content := range okCases {
		t.Run(name, func(t *testing.T) {
			if got, ok := duplicateObjectName(strings.NewReader(content)); ok {
				t.Fatalf("%s：不应判为字段重复，却报字段 %q\n%s", name, got, content)
			}
		})
	}

	// 语法错误由正式解析阶段负责，扫描器不应抢先报“重复字段”。
	for name, content := range map[string]string{
		"未写完的对象": `{"credit":4,`,
		"普通文字":   `普通文字`,
		"尾部多余括号": `{"a":1}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if got, ok := duplicateObjectName(strings.NewReader(content)); ok {
				t.Fatalf("%s：语法错误不应报成重复字段 %q", name, got)
			}
		})
	}
}

// TestLoadRejectsDuplicateObjectFields 任何一层对象出现重复字段，整份文件
// 都按内容损坏拒绝：错误信息点名文件与重复字段，原文件字节原样保留，
// 即使其余学生的记录完全合法也不能只加载一部分。
func TestLoadRejectsDuplicateObjectFields(t *testing.T) {
	goodEnroll := `{"student":"s1","req":"r1","term":"2024春","id":"e1","result":"enrolled"}`
	cases := map[string]struct {
		content   string
		fieldName string
	}{
		"课程学分重复": {
			`{"version":1,"courses":[` +
				`{"id":"c1","name":"数学","credit":4,"open":true,"credit":9}` +
				`],"students":[],"requirements":[],"enrollments":[],"waivers":[],"nextResultSeq":0}`,
			"credit",
		},
		"修读学生重复且其余学生合法": {
			`{"version":1,"courses":[` +
				`{"id":"c1","name":"数学","credit":4,"open":true}],` +
				`"students":[{"id":"s1"},{"id":"s2"}],` +
				`"requirements":[{"id":"r1","student":"s1","course":"c1"}],` +
				`"enrollments":[` + goodEnroll + `,` +
				`{"student":"s2","student":"s9","req":"rX","id":"eX","term":"2024春","result":"enrolled"}],` +
				`"waivers":[],"nextResultSeq":0}`,
			"student",
		},
		"顶层两份courses": {
			`{"version":1,"courses":[],"courses":[]}`,
			"courses",
		},
		"Unicode转义重复学分": {
			"{\"version\":1,\"courses\":[{\"id\":\"c1\",\"name\":\"数学\"," +
				"\"credit\":4,\"\\u0063redit\":9,\"open\":true}]}",
			"credit",
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
				t.Fatalf("%s：含重复字段的文件应判损坏拒绝，却得到 store=%v", name, s)
			}
			if !existed {
				t.Fatalf("%s：损坏文件应标记为已存在", name)
			}
			msg := err.Error()
			if !strings.Contains(msg, path) {
				t.Fatalf("%s：错误信息应点名文件 %s，得到：%v", name, path, err)
			}
			if !strings.Contains(msg, "内容损坏") {
				t.Fatalf("%s：错误信息应说明内容损坏，得到：%v", name, err)
			}
			if !strings.Contains(msg, tc.fieldName) {
				t.Fatalf("%s：错误信息应指出重复字段 %q，得到：%v", name, tc.fieldName, err)
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != tc.content {
				t.Fatalf("%s：拒绝读取不得改写原文件\nwant=%q\n got=%q", name, tc.content, got)
			}
		})
	}
}

// TestLoadWorksAfterDuplicateFieldFixed 修正重复字段后的合法记录仍走原入口
// 正常读取，学分与归属按修正后的内容计算，规则保持原样。
func TestLoadWorksAfterDuplicateFieldFixed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "records.json")
	bad := `{"version":1,"courses":[` +
		`{"id":"c1","name":"数学","credit":4,"credit":9,"open":true}],"students":[],` +
		`"requirements":[],"enrollments":[],"waivers":[],"nextResultSeq":0}`
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil {
		t.Fatal("修正前应拒绝")
	}
	fixed := `{"version":1,"courses":[` +
		`{"id":"c1","name":"数学","credit":4,"open":true}],` +
		`"students":[{"id":"s1"}],` +
		`"requirements":[{"id":"r1","student":"s1","course":"c1"}],` +
		`"enrollments":[],"waivers":[],"nextResultSeq":0}`
	if err := os.WriteFile(path, []byte(fixed), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("修正后应正常读取：existed=%v err=%v", existed, err)
	}
	if c := loaded.Course("c1"); c == nil || c.Credit != 4 {
		t.Fatalf("修正后学分应为 4，得到 %+v", c)
	}
}

// TestLoadDuplicateFieldNameInValueOK 依据或课程名称的“文字内容”里出现
// 字段名不构成重复，记录正常读取。
func TestLoadDuplicateFieldNameInValueOK(t *testing.T) {
	s := NewStore()
	mustStudent(t, s, "s1")
	mustCourse(t, s, "c1", "credit 学分导论", 4)
	mustReq(t, s, "s1", "r1", "c1")
	if _, _, err := s.ApplyWaiver("s1", "r1", "w1", "student 编号以学籍系统为准"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "records.json")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("值内容提到字段名不应判损坏：%v", err)
	}
	if c := loaded.Course("c1"); c == nil || c.Name != "credit 学分导论" {
		t.Fatalf("课程名称未原样恢复：%+v", c)
	}
	if w := loaded.Waiver("s1", "w1"); w == nil || w.Basis != "student 编号以学籍系统为准" {
		t.Fatalf("免修依据未原样恢复：%+v", w)
	}
}
