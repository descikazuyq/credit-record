package credit

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"
)

// duplicateFieldError 表示同一个 JSON 对象内出现了读取功能无法确定归属的
// 两个字段：
//   - 字段名完全相同（first == second，如两份 "credit"）；
//   - 两个字段名只有大小写差异（编码/json 的大小写折叠等同于
//     strings.EqualFold），却都被现有读取功能识别为同一条记录字段
//     （如 "credit" 与 "Credit" 都填入课程学分，"courses" 与
//     "COURSES" 都填入最外层课程列表）。
//
// field 是两者共同指向的规范字段名（结构体 json 标签原文）；完全同名时
// 与 first、second 相同。
type duplicateFieldError struct {
	field  string
	first  string
	second string
}

func (e *duplicateFieldError) Error() string {
	if e.first == e.second {
		return fmt.Sprintf("同一 JSON 对象内字段名 %q 重复出现，字段归属无法确定", e.first)
	}
	return fmt.Sprintf(
		"同一 JSON 对象内字段名 %q 与 %q 仅大小写不同，读取时都指向同一记录字段 %q，字段归属无法确定",
		e.first, e.second, e.field)
}

// recordSchema 描述一个 JSON 对象在记录格式中的字段识别口径：该对象有哪些
// 能被现有读取功能识别的字段名（json 标签原文），以及其中每个“记录数组”
// 字段的元素对象是哪种 schema（顶层 courses 的元素是课程，依此类推）。
//
// 正常读取（encoding/json 解进结构体）与读取前的字段归属检查只共用这一份
// 描述：调整记录结构体的字段定义（增删字段、改动 json 标签、增删一类记录）
// 时，能识别哪些写法、哪些写法指向同一字段的判断随之一起改变，不需要再
// 同步修改另一份手写字段名称清单。
type recordSchema struct {
	// fields 是本对象能被读取功能识别的字段名（结构体 json 标签原文）。
	// 同一对象的标签在大小写不敏感比较下两两不同（否则该格式本身就有歧义）。
	fields []string
	// arrays 以 json 标签原文为键，记录“记录数组”字段元素对象的 schema；
	// 非数组字段（version 等标量）不在此表。
	arrays map[string]*recordSchema
}

// fileSchema 是记录格式唯一的字段识别口径，直接从磁盘结构体定义推导：根为
// fileData，沿其中的记录切片字段到达课程、学生、课程要求、修读、免修各结构
// 体。识别字段名一律取结构体字段的 json 标签原文——与 encoding/json 正常
// 解码所用的名称完全同源，不存在第二份需要同步维护的清单。推导只依赖类型
// 定义、不依赖任何文件内容，包初始化期间一次性完成。
var fileSchema = newRecordSchema(reflect.TypeOf(fileData{}))

// newRecordSchema 从结构体类型 t 推导其 JSON 对象的字段识别口径：字段名取
// 每个可导出字段 json 标签里的名称（",omitempty" 等选项剔除），名称为 "-"
// 的字段不参与识别；元素类型也是结构体的切片字段登记为记录数组，其元素
// schema 按同一方式递归推导。
//
// 只识别“结构体元素切片”这一种嵌套：记录格式只有最外层的五个数组承载
// 记录对象，其它嵌套对象在格式中不存在，遇到时按未知对象交给结构体解码
// 阶段拒绝。
func newRecordSchema(t reflect.Type) *recordSchema {
	sc := &recordSchema{arrays: map[string]*recordSchema{}}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // 不可导出字段不参与 JSON 解码
		}
		name, skip := jsonFieldName(f)
		if skip {
			continue // json:"-" 显式不参与
		}
		sc.fields = append(sc.fields, name)
		if f.Type.Kind() == reflect.Slice {
			e := f.Type.Elem()
			if e.Kind() == reflect.Ptr {
				e = e.Elem()
			}
			if e.Kind() == reflect.Struct {
				sc.arrays[name] = newRecordSchema(e)
			}
		}
	}
	return sc
}

// jsonFieldName 取结构体字段在 JSON 中的名称，规则与 encoding/json 完全
// 一致：标签缺失或名称为空（如 `json:",omitempty"`）时退化为 Go 字段名；
// 名称为 "-" 且没有选项（`json:"-"`）表示该字段不参与（skip=true），而
// `json:"-,"` 是一个名称恰为 "-" 的正常字段。
func jsonFieldName(f reflect.StructField) (name string, skip bool) {
	tag, ok := f.Tag.Lookup("json")
	if !ok {
		return f.Name, false
	}
	name, _, _ = strings.Cut(tag, ",")
	if name == "-" && !strings.Contains(tag, ",") {
		return "", true
	}
	if name == "" {
		return f.Name, false
	}
	return name, false
}

// matchField 报告键名 key 是否会被现有读取功能填入该 schema 对象的某个
// 记录字段，命中时返回该字段的规范名称（json 标签原文）。
//
// encoding/json 解进结构体时先按精确名匹配，不中再按大小写折叠名匹配，
// 折叠规则等同于 strings.EqualFold（完整 Unicode 简单折叠，而非仅 ASCII：
// 例如 U+212A“K”折到 k、U+0130“İ”折到 i）。这里照搬同一判定，才能保证
// “两个键名被现有读取功能识别为同一字段”的口径与实际解码完全一致——
// 任何大小写或 Unicode 转义组合都无法让一个键绕过本扫描却仍被解码器接受。
// 字段清单来自结构体定义本身（见 fileSchema），与正常解码同源。
func (sc *recordSchema) matchField(key string) (string, bool) {
	if sc == nil {
		return "", false
	}
	for _, tag := range sc.fields {
		if strings.EqualFold(key, tag) {
			return tag, true
		}
	}
	return "", false
}

// arrayElementSchema 给出本对象某个键所对应记录数组的元素 schema；该键不
// 指向记录数组（不是可识别字段、或只是 version 之类标量）时返回 nil。
// 键名按“能否被读取功能识别”为准：大小写折叠后等于 courses 的键（如
// COURSES）解出来仍是课程列表，其元素必须按课程对象检查，不能借顶层键
// 的大小写写法绕过限制。
func (sc *recordSchema) arrayElementSchema(key string) *recordSchema {
	if sc == nil {
		return nil
	}
	canonical, ok := sc.matchField(key)
	if !ok {
		return nil
	}
	return sc.arrays[canonical]
}

// scanDuplicateKeys 扫描第一份完整 JSON 值的 token 流，在每个对象内检查
// 字段归属唯一；第一份值之后的字节一律不读（调用方另行检查完整记录之后
// 是否还拼接了多余内容，那里的报错信息需要与本检查区分开）。
//
// 背景：encoding/json 直接解进结构体（或 map）时，同一对象内的重复键会被
// 后一个静默覆盖，而且结构体字段按大小写折叠匹配 json 标签（规则等同于
// strings.EqualFold）——同一门课程同时写 "credit":4 与 "Credit":9 时，
// 标准库读不出“曾经写过两个”，学分直接被后一个值顶替，两个键先后颠倒
// 结果也随之颠倒。因此在正常结构体解码之前先单独扫描一遍 token 流。
//
// 规则：
//   - 同一个 JSON 对象内字段归属只能确定一次：完全同名的两个键，或两个
//     键名在大小写折叠后都指向同一条可识别记录字段（如 credit 与
//     Credit、student 与 Student、courses 与 COURSES），发现即返回
//     *duplicateFieldError（含规范字段名与两个原始写法）。
//   - 字段顺序、是否相邻、两个值是否相同都不影响判定。
//   - 比较以 JSON 解码后的文字为准：Token() 返回的键名已完成 Unicode
//     反转义，所以 "credit"、它的 \uXXXX 转义写法与 "Credit" 之间任意
//     两两组合都按解码后实际所指的字段判断，不能用转义加大小写的组合
//     绕开限制。
//   - 限制只针对同一对象：对象与数组可任意嵌套，数组各元素、不同课程或
//     不同学生等不同对象各自携带同名字段是正常结构，互不比较。
//   - 不能被读取功能识别的字段不参与折叠归并，但完全同名仍按重复拒绝；
//     真正的未知字段留给随后的 DisallowUnknownFields 解码阶段拒绝。
//   - 字符串“值”里出现字段名（依据、课程名称等）不是键，不参与检查。
func scanDuplicateKeys(r io.Reader) error {
	type frame struct {
		isObj bool
		// schema 标识对象能识别哪些字段（对象帧使用）；不属于记录格式的
		// 对象为 nil，其键不参与大小写归并（完全同名仍按重复拒绝）。
		schema *recordSchema
		// elem 标识数组元素对象的 schema（数组帧使用）；非记录数组为 nil。
		elem *recordSchema
		// expectKey 仅对对象有意义：true 表示下一个字符串 token 是键
		// （或直接遇到 '}'）；false 表示正在等待该键所对应的值。
		// 对象内 token 天然按键、值、键、值交替（逗号不产生 token），
		// 借此区分字符串 token 是键还是普通字符串值。
		expectKey bool
		// pendingKey 是当前值所属的键，用于判断嵌套对象/数组的类型。
		pendingKey string
		// rawKeys 记录该对象内出现过的全部原始键名（解码后文字）。
		rawKeys map[string]struct{}
		// seen 记录每条可识别记录字段第一次出现时的原始写法，以规范
		// 字段名为索引；无法识别的键不进此表。
		seen map[string]string
	}
	var stack []frame
	depth := 0

	push := func(d json.Delim) {
		switch d {
		case '{':
			var sc *recordSchema
			if n := len(stack); n == 0 {
				// 第一份值的根对象即记录最外层；根值不是对象时结构体
				// 解码阶段自会按类型拒绝。
				sc = fileSchema
			} else if p := &stack[n-1]; p.isObj {
				// 嵌套对象的字段口径由父对象当前键对应的记录数组给出；
				// 该键不承载记录数组时为 nil（记录格式中不存在这种对象，
				// 交由结构体解码按未知字段/类型拒绝）。
				sc = p.schema.arrayElementSchema(p.pendingKey)
			} else {
				// 对象在数组里：字段口径由承载它的数组决定（数组口径又由
				// 最外层五个字段之一决定）。
				sc = stack[n-1].elem
			}
			stack = append(stack, frame{
				isObj: true, schema: sc, expectKey: true,
				rawKeys: map[string]struct{}{},
				seen:    map[string]string{},
			})
		case '[':
			var elem *recordSchema
			if n := len(stack); n > 0 {
				if p := &stack[n-1]; p.isObj {
					elem = p.schema.arrayElementSchema(p.pendingKey)
				} else {
					// 数组嵌套：内层数组元素类型跟随外层数组。
					elem = p.elem
				}
			}
			stack = append(stack, frame{isObj: false, elem: elem})
		}
	}
	// valueEnded 通知栈顶对象：刚读完一个值（标量或嵌套结构的结束括号）。
	valueEnded := func() {
		if n := len(stack); n > 0 {
			f := &stack[n-1]
			if f.isObj && !f.expectKey {
				f.expectKey = true
				f.pendingKey = ""
			}
		}
	}

	dec := json.NewDecoder(r)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			// 空输入交由后续结构体解码报错（空文件在更前面已被拒绝）。
			return nil
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{', '[':
				depth++
				push(t)
			case '}', ']':
				depth--
				stack = stack[:len(stack)-1]
				if depth == 0 {
					// 唯一的顶层值已完整读完，其后内容不在本检查范围。
					return nil
				}
				// 刚结束的嵌套对象/数组本身就是外层键所对应的值。
				valueEnded()
			}
		case string:
			n := len(stack)
			if n > 0 && stack[n-1].isObj && stack[n-1].expectKey {
				f := &stack[n-1]
				// 对象键：Token() 已按解码后的文字返回，\uXXXX 转义
				// 形式的键名与直接写出的同名键在这里完全不可区分。
				if _, dup := f.rawKeys[t]; dup {
					return &duplicateFieldError{field: t, first: t, second: t}
				}
				f.rawKeys[t] = struct{}{}
				// 只有指向同一条可识别记录字段的不同写法才算归属
				// 冲突；未知键的大小写变体随后由结构体解码按未知字段
				// 拒绝（如 "foo" 与 "Foo" 不会被当成任何业务字段）。
				// 字段清单直接来自结构体定义（见 fileSchema），正常解码
				// 与本检查对“能识别哪些写法”的口径始终一致；schema 为
				// nil 的未知对象不参与归并（matchField 对 nil 返回 false）。
				if canonical, known := f.schema.matchField(t); known {
					if prev, conflict := f.seen[canonical]; conflict {
						return &duplicateFieldError{
							field:  canonical,
							first:  prev,
							second: t,
						}
					}
					f.seen[canonical] = t
				}
				f.expectKey = false
				f.pendingKey = t
				continue
			}
			valueEnded()
			if depth == 0 {
				return nil // 顶层标量值
			}
		default:
			// number、bool、nil 等标量值。
			valueEnded()
			if depth == 0 {
				return nil
			}
		}
	}
}

// scanStringUnicode 按原始字节扫描第一份完整 JSON 值里的全部字符串
// （字段名与字段值），验证每个字符串都能还原为合法 Unicode 文字：
//   - 字符串中直接写入的字节必须是合法 UTF-8；
//   - \uXXXX 转义中的高位代理项（U+D800–U+DBFF）必须紧跟一个低位代理项
//     （U+DC00–U+DFFF）转义；孤立的低位代理项同样非法。
//
// 背景：encoding/json 解码时会把非法 UTF-8 字节与未配对的代理项转义静默
// 替换成 U+FFFD（“�”）。替换后的学生编号、课程名称或免修依据绝不能被当成
// 正常内容继续查询、核对或登记——两个本来不同的编号可能被替换成同一个
// 而被误判重复或误判相同。因此必须在结构体解码之前按原始字节拒绝这样的
// 文件，整份判为内容损坏，哪怕问题只在一条已拒绝免修的依据里、或在与本次
// 核对无关的课程名称里。
//
// 合法文字不受影响：中文、直接写入的补充平面字符、完整的高低代理项转义对、
// 用转义表达的控制字符都保留原意；用户确实写入的“�”（U+FFFD，直接写出
// 或 \uFFFD 转义）也是合法文字，不能仅因解码结果含有它就拒绝文件。
//
// 与 scanDuplicateKeys 一样只扫描第一份完整值：完整记录之后的多余内容
// 本来就会被读取方拒绝，无需在此重复判定。
func scanStringUnicode(raw []byte) error {
	i := 0
	for i < len(raw) && isJSONSpace(raw[i]) {
		i++
	}
	if i >= len(raw) {
		return nil // 空输入交由后续结构体解码报错
	}
	switch raw[i] {
	case '"':
		// 顶层就是一个字符串：扫描它本身即可。
		_, err := scanJSONString(raw, i)
		return err
	case '{', '[':
	default:
		return nil // 顶层标量（数字、true、false、null）不含字符串
	}
	depth := 0
	for i < len(raw) {
		switch raw[i] {
		case '"':
			j, err := scanJSONString(raw, i)
			if err != nil {
				return err
			}
			i = j
		case '{', '[':
			depth++
			i++
		case '}', ']':
			depth--
			i++
			if depth == 0 {
				// 唯一的顶层值已完整读完，其后内容不在本检查范围。
				return nil
			}
		default:
			i++
		}
	}
	return nil // 未闭合的结构由 JSON 解析报错
}

// scanJSONString 扫描从 raw[start]（必须是 '"'）开始的一个字符串字面量，
// 返回结束引号之后的下标。字符串内容无法还原为合法 Unicode 文字时报错；
// 未闭合、非法转义形式等语法问题留给 JSON 解析器报告，本函数不为此报错。
func scanJSONString(raw []byte, start int) (int, error) {
	i := start + 1
	for i < len(raw) {
		c := raw[i]
		switch {
		case c == '"':
			return i + 1, nil
		case c == '\\':
			if i+1 >= len(raw) {
				return len(raw), nil
			}
			if raw[i+1] != 'u' {
				i += 2 // 其他转义的合法性由 JSON 解析器校验
				continue
			}
			v, ok := hex4(raw, i+2)
			if !ok {
				i += 2 // 非法 \u 转义形式由 JSON 解析器报错
				continue
			}
			switch {
			case isHighSurrogate(v):
				// 高位代理项必须紧跟一个 \uXXXX 形式的低位代理项转义。
				if i+12 <= len(raw) && raw[i+6] == '\\' && raw[i+7] == 'u' {
					if v2, ok := hex4(raw, i+8); ok && isLowSurrogate(v2) {
						i += 12
						continue
					}
				}
				return 0, fmt.Errorf(
					`高位代理项转义 \u%04X 后没有紧跟低位代理项转义`, v)
			case isLowSurrogate(v):
				return 0, fmt.Errorf(
					`低位代理项转义 \u%04X 没有对应的高位代理项转义`, v)
			default:
				i += 6
			}
		case c < utf8.RuneSelf:
			i++
		default:
			// 直接写入的多字节字符必须是合法 UTF-8。注意 U+FFFD 本身
			// 是合法文字：只有解码失败（size 为 1）才拒绝。
			r, size := utf8.DecodeRune(raw[i:])
			if r == utf8.RuneError && size == 1 {
				return 0, fmt.Errorf(
					"字符串中直接写入的字节 0x%02X 不是合法 UTF-8", c)
			}
			i += size
		}
	}
	return i, nil // 未闭合的字符串由 JSON 解析器报错
}

func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func isHighSurrogate(r rune) bool { return 0xD800 <= r && r <= 0xDBFF }
func isLowSurrogate(r rune) bool  { return 0xDC00 <= r && r <= 0xDFFF }

// hex4 把 b[i:i+4] 读作四位十六进制数。
func hex4(b []byte, i int) (rune, bool) {
	if i+4 > len(b) {
		return 0, false
	}
	var v rune
	for k := 0; k < 4; k++ {
		c := b[i+k]
		var d byte
		switch {
		case '0' <= c && c <= '9':
			d = c - '0'
		case 'a' <= c && c <= 'f':
			d = c - 'a' + 10
		case 'A' <= c && c <= 'F':
			d = c - 'A' + 10
		default:
			return 0, false
		}
		v = v*16 + rune(d)
	}
	return v, true
}
