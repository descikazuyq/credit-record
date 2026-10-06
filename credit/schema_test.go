package credit

import (
	"reflect"
	"testing"
)

// TestFileSchemaDerivedFromRecordDefinitions 字段识别口径必须直接来自记录结构
// 体定义本身：根为 fileData，五个记录切片字段各自连到对应的记录结构体，
// 可识别字段名就是这些结构体字段的 json 标签原文（omitempty 等选项不算
// 名称）。这样正常解码与读取前的重复字段检查共用同一份字段定义，不存在
// 需要另行同步的手写清单。
func TestFileSchemaDerivedFromRecordDefinitions(t *testing.T) {
	// 直接按结构体定义独立计算一遍期望口径，与 fileSchema 对照，而不是在
	// 测试里再写一份文件格式字段名清单。
	wantRoot := newRecordSchema(reflect.TypeOf(fileData{}))
	if !reflect.DeepEqual(fileSchema.fields, wantRoot.fields) {
		t.Fatalf("根对象可识别字段=%v，应来自 fileData 定义 %v",
			fileSchema.fields, wantRoot.fields)
	}
	wantArrays := map[string]reflect.Type{
		"courses":      reflect.TypeOf(Course{}),
		"students":     reflect.TypeOf(Student{}),
		"requirements": reflect.TypeOf(Requirement{}),
		"enrollments":  reflect.TypeOf(Enrollment{}),
		"waivers":      reflect.TypeOf(Waiver{}),
	}
	if len(fileSchema.arrays) != len(wantArrays) {
		t.Fatalf("记录数组数量=%d，期望 %d（%v）",
			len(fileSchema.arrays), len(wantArrays), fileSchema.arrays)
	}
	for key, typ := range wantArrays {
		elem := fileSchema.arrays[key]
		if elem == nil {
			t.Fatalf("顶层字段 %q 应承载记录数组", key)
		}
		wantElem := newRecordSchema(typ)
		if !reflect.DeepEqual(elem.fields, wantElem.fields) {
			t.Fatalf("%s 元素可识别字段=%v，应来自 %s 定义 %v",
				key, elem.fields, typ.Name(), wantElem.fields)
		}
		if len(elem.arrays) != 0 {
			t.Fatalf("记录结构体 %s 不应再嵌套记录数组，得到 %v", typ.Name(), elem.arrays)
		}
	}
	// 标量字段不产生数组元素口径。
	if fileSchema.arrayElementSchema("version") != nil {
		t.Fatal("version 是标量，不应有数组元素口径")
	}
	if fileSchema.arrayElementSchema("nextResultSeq") != nil {
		t.Fatal("nextResultSeq 是标量，不应有数组元素口径")
	}
	// 顶层键的大小写折叠与正常解码规则一致：COURSES 的元素仍是课程对象。
	if fileSchema.arrayElementSchema("COURSES") != fileSchema.arrays["courses"] {
		t.Fatal("COURSES 折叠后应指向课程数组的元素口径")
	}
}

// TestRecordSchemaTracksStructDefinition 字段识别口径随结构体定义自动变化：
// json 标签（含 omitempty 选项）、字段名、嵌套记录切片都由定义推导，新增或
// 改动一个字段不需要再修改任何手写名称清单。这里用一个独立的合成结构体
// 验证推导规则本身。
func TestRecordSchemaTracksStructDefinition(t *testing.T) {
	type inner struct {
		A  string `json:"a"`
		B  string `json:"b,omitempty"`
		ID string // 无标签：按 encoding/json 规则退化为字段名
		S  string `json:"-"`  // 显式不参与
		L  string `json:"-,"` // 名称恰为 "-" 的正常字段
		E  string `json:",omitempty"`
	}
	type outer struct {
		Version int      `json:"version"`
		Inners  []*inner `json:"inners"`
		Other   []string `json:"other"` // 非结构体元素：不是记录数组
	}
	sc := newRecordSchema(reflect.TypeOf(outer{}))
	wantFields := []string{"version", "inners", "other"}
	if !reflect.DeepEqual(sc.fields, wantFields) {
		t.Fatalf("outer 字段=%v，期望 %v", sc.fields, wantFields)
	}
	if _, ok := sc.matchField("VERSION"); !ok {
		t.Fatal("VERSION 应按大小写折叠识别到 version")
	}
	if _, ok := sc.matchField("missing"); ok {
		t.Fatal("不存在的字段不应被识别")
	}
	elem := sc.arrayElementSchema("INNERS")
	if elem == nil {
		t.Fatal("inners 应按折叠后的键找到记录数组元素口径")
	}
	wantInner := []string{"a", "b", "ID", "-", "E"}
	if !reflect.DeepEqual(elem.fields, wantInner) {
		t.Fatalf("inner 字段=%v，期望 %v（omitempty 选项与 json:\"-\" 应剔除，"+
			"json:\"-,\" 与空名退化应保留）", elem.fields, wantInner)
	}
	if canon, ok := elem.matchField("A"); !ok || canon != "a" {
		t.Fatalf("A 应折叠到规范字段 a，得到 (%q,%v)", canon, ok)
	}
	if _, ok := elem.matchField("S"); ok {
		t.Fatal("json:\"-\" 字段不应参与识别")
	}
	if canon, ok := elem.matchField("-"); !ok || canon != "-" {
		t.Fatalf("json:\"-,\" 是名称为 \"-\" 的正常字段，得到 (%q,%v)", canon, ok)
	}
	if sc.arrayElementSchema("other") != nil {
		t.Fatal("非结构体元素切片不是记录数组")
	}
	if sc.arrayElementSchema("version") != nil {
		t.Fatal("标量字段不是记录数组")
	}
}
