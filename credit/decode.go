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

// nullCourseOpenError 表示课程对象显式提供了开放状态字段，却把值写成 JSON
// 的 null（"open": null）。encoding/json 把 null 解进非指针 bool 字段时会
// 静默保留零值 false：未定状态会被读成明确的“停开”，课程列表显示停开、
// 新增修读因此被拒，之后任何需要保存的操作还会把 false 落盘，让没有确定
// 状态的数据变成明确的停开记录。这不是合法状态，按课程记录内容损坏处理。
// courseID 是该课程对象中 id 字段解码后的编号原文（id 出现在 open 前后均
// 可，扫描在课程对象结束时才报错以取得编号）；课程同时缺少编号时为空串。
type nullCourseOpenError struct {
	courseID string
}

func (e *nullCourseOpenError) Error() string {
	if e.courseID == "" {
		return "存在开放状态为空的课程（该课程同时缺少编号）：open 字段被显式写成 null，" +
			"开放状态只能是 true（开放）或 false（停开）"
	}
	return fmt.Sprintf(
		"课程 %s 的开放状态为空：open 字段被显式写成 null，开放状态只能是 true（开放）或 false（停开）",
		e.courseID)
}

// objKind 标识一个 JSON 对象在记录格式中的位置，用于确定该对象有哪些
// “能被现有读取功能识别”的字段名。
type objKind int

const (
	objUnknown     objKind = iota // 不属于记录格式的对象（交由结构体解码按未知字段/类型拒绝）
	objFile                       // 记录最外层 fileData
	objCourse                     // courses 数组元素：课程
	objStudent                    // students 数组元素：学生
	objRequirement                // requirements 数组元素：课程要求
	objEnrollment                 // enrollments 数组元素：修读
	objWaiver                     // waivers 数组元素：免修
)

// recordObjectTypes 把每类对象对应到它在记录格式中的结构体类型。各类对象
// “能被读取功能识别哪些字段名”只有结构体定义上的 json 标签这一个出处：
// 正常读取时 json.Decoder 按这些标签（先精确名、再大小写折叠名）填入字段，
// 重复字段扫描也从同一份结构体定义取字段名。调整一项记录的字段（增删、
// 改名、换标签）只需改结构体定义，正常读取与重复检查始终对“哪些字段可
// 识别、哪些写法指向同一字段”作出一致判断，不需要再同步另一份字段名称
// 清单。
var recordObjectTypes = map[objKind]reflect.Type{
	objFile:        reflect.TypeOf(fileData{}),
	objCourse:      reflect.TypeOf(Course{}),
	objStudent:     reflect.TypeOf(Student{}),
	objRequirement: reflect.TypeOf(Requirement{}),
	objEnrollment:  reflect.TypeOf(Enrollment{}),
	objWaiver:      reflect.TypeOf(Waiver{}),
}

// jsonFieldName 按 encoding/json 的规则给出结构体字段被识别的 JSON 名称：
// 未导出字段不参与解码；json 标签为 "-" 的字段跳过（返回空串）；没有标签
// 时用 Go 字段名本身；omitempty 等选项不影响名称。记录结构体均为扁平字段，
// 无嵌入字段提升问题。
func jsonFieldName(sf reflect.StructField) string {
	if sf.PkgPath != "" {
		return ""
	}
	name := sf.Tag.Get("json")
	if name == "-" {
		return ""
	}
	if comma := strings.IndexByte(name, ','); comma >= 0 {
		name = name[:comma]
	}
	if name == "" {
		name = sf.Name
	}
	return name
}

// recordFields 是各类对象能被读取功能识别的字段名（结构体 json 标签原文），
// 全部直接取自 recordObjectTypes 中的结构体定义，不在别处另列一份。
// 同一对象的字段名在大小写不敏感比较下必须两两不同——否则该格式本身就有
// 歧义（两个字段折叠到同一名称），在初始化阶段直接指出这类定义错误。
var recordFields = buildRecordFields()

func buildRecordFields() map[objKind][]string {
	m := make(map[objKind][]string, len(recordObjectTypes))
	for kind, t := range recordObjectTypes {
		names := make([]string, 0, t.NumField())
		for i := 0; i < t.NumField(); i++ {
			name := jsonFieldName(t.Field(i))
			if name == "" {
				continue
			}
			for _, other := range names {
				if strings.EqualFold(name, other) {
					panic(fmt.Sprintf(
						"记录结构体 %s 的字段 %q 与 %q 在大小写折叠后同名，记录格式存在歧义",
						t.Name(), name, other))
				}
			}
			names = append(names, name)
		}
		m[kind] = names
	}
	return m
}

// matchRecordField 报告键名 key 是否会被现有读取功能填入 kind 对象的某个
// 记录字段，命中时返回该字段的规范名称（json 标签原文）。
//
// encoding/json 解进结构体时先按精确名匹配，不中再按大小写折叠名匹配，
// 折叠规则等同于 strings.EqualFold（完整 Unicode 简单折叠，而非仅 ASCII：
// 例如 U+212A“K”折到 k、U+0130“İ”折到 i）。这里照搬同一判定，字段名又
// 直接来自结构体标签，才能保证“两个键名被现有读取功能识别为同一字段”的
// 口径与实际解码完全一致——任何大小写或 Unicode 转义组合都无法让一个键
// 绕过本扫描却仍被解码器接受。
func matchRecordField(kind objKind, key string) (string, bool) {
	for _, tag := range recordFields[kind] {
		if strings.EqualFold(key, tag) {
			return tag, true
		}
	}
	return "", false
}

// fileKeyObjectKind 给出最外层对象某个键所对应数组元素/嵌套对象的类型。
// 记录格式里只有最外层的五个数组承载记录对象。归属同样直接由 fileData 的
// 结构体定义推导：键按大小写折叠命中哪个字段，就取该字段（切片取元素、
// 指针解引用）的类型再反查对象类型——courses 与 COURSES 解出来都是课程
// 列表，其元素必须按课程对象检查，不能借顶层键的大小写写法绕过限制；
// version 等非记录对象字段返回 objUnknown。
func fileKeyObjectKind(key string) objKind {
	t := recordObjectTypes[objFile]
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		name := jsonFieldName(sf)
		if name == "" || !strings.EqualFold(name, key) {
			continue
		}
		ft := sf.Type
		if ft.Kind() == reflect.Slice {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		for kind, rt := range recordObjectTypes {
			if rt == ft {
				return kind
			}
		}
		return objUnknown
	}
	return objUnknown
}

// scanDuplicateKeys 扫描第一份完整 JSON 值的 token 流，在每个对象内检查
// 字段归属唯一，同时检查课程开放状态字段不被显式写成 null；第一份值之后的
// 字节一律不读（调用方另行检查完整记录之后是否还拼接了多余内容，那里的报错
// 信息需要与本检查区分开）。
//
// 背景一（字段归属）：encoding/json 直接解进结构体（或 map）时，同一对象
// 内的重复键会被后一个静默覆盖，而且结构体字段按大小写折叠匹配 json 标签（规则等同于
// strings.EqualFold）——同一门课程同时写 "credit":4 与 "Credit":9 时，
// 标准库读不出“曾经写过两个”，学分直接被后一个值顶替，两个键先后颠倒
// 结果也随之颠倒。因此在正常结构体解码之前先单独扫描一遍 token 流。
//
// 背景二（课程开放状态）：课程的开放状态字段是非指针 bool，encoding/json
// 遇到显式 null（"open": null）时不会报错，而是静默保留零值 false——没有
// 确定的状态会被读成明确的“停开”：课程列表显示停开、新增修读因此被拒，
// 之后任何需要保存的操作还会把 false 写回文件，让未定状态变成明确的停开
// 记录。因此课程对象一旦显式给出指向 open 的字段而值为 null，就在本扫描
// 中按内容损坏拒绝整份文件，哪怕这门课尚未被要求引用、或本次办理的是另一
// 名学生，也不能跳过该课程继续办理。
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
//     绕开限制。open/Open/OPEN 及其 \uXXXX 转义写法同理：解码后等同于
//     这些名字的键都指向课程开放状态，课程对象中对应值为 null 时返回
//     *nullCourseOpenError；true 与 false 是合法状态，省略 open 字段不
//     在此列（沿用既有读取规则）。
//   - 开放状态检查只认课程对象（objCourse）内指向 open 的字段：课程名称、
//     免修依据等字符串值里的普通文字“null”不是字段值；其他对象中无法
//     识别的 open 写法留给随后的未知字段解码阶段处理。字段在课程对象中
//     的位置不影响结果：open 出现在 id 之前时，错误延迟到该课程对象结束
//     再报告，以便点名课程编号。最外层表示“没有记录”的 null 列表或空
//     数组（如 "courses": null）不是课程对象，不参与本检查。
//   - 限制只针对同一对象：对象与数组可任意嵌套，数组各元素、不同课程或
//     不同学生等不同对象各自携带同名字段是正常结构，互不比较。
//   - 不能被读取功能识别的字段不参与折叠归并，但完全同名仍按重复拒绝；
//     真正的未知字段留给随后的 DisallowUnknownFields 解码阶段拒绝。
//   - 字符串“值”里出现字段名（依据、课程名称等）不是键，不参与检查。
func scanDuplicateKeys(r io.Reader) error {
	type frame struct {
		isObj bool
		// kind 标识对象类型（决定有哪些可识别字段）；数组帧不用。
		kind objKind
		// elem 标识数组元素对象的类型（数组帧使用）。
		elem objKind
		// expectKey 仅对对象有意义：true 表示下一个字符串 token 是键
		// （或直接遇到 '}'）；false 表示正在等待该键所对应的值。
		// 对象内 token 天然按键、值、键、值交替（逗号不产生 token），
		// 借此区分字符串 token 是键还是普通字符串值。
		expectKey bool
		// pendingKey 是当前值所属的键，用于判断嵌套对象/数组的类型。
		pendingKey string
		// pendingCanonical 是当前值所属键对应的规范记录字段名（不指向任何
		// 可识别记录字段时为空），用于识别课程 open 字段的显式 null 值。
		pendingCanonical string
		// rawKeys 记录该对象内出现过的全部原始键名（解码后文字）。
		rawKeys map[string]struct{}
		// seen 记录每条可识别记录字段第一次出现时的原始写法，以规范
		// 字段名为索引；无法识别的键不进此表。
		seen map[string]string
		// nullOpen 标记课程对象已出现“显式 open 字段值为 null”。发现时不
		// 立即报错，延迟到对象结束，以便用课程编号点名。
		nullOpen bool
		// courseID 是课程对象中 id 字段解码后的编号原文，供 open 出现在 id
		// 之前时的错误信息点名课程；缺少编号则保持空串。
		courseID string
	}
	var stack []frame
	depth := 0

	push := func(d json.Delim) {
		switch d {
		case '{':
			kind := objUnknown
			if n := len(stack); n == 0 {
				// 第一份值的根对象即记录最外层；根值不是对象时结构体
				// 解码阶段自会按类型拒绝。
				kind = objFile
			} else if p := &stack[n-1]; p.isObj {
				if p.kind == objFile {
					kind = fileKeyObjectKind(p.pendingKey)
				}
			} else {
				// 对象在数组里：类型由承载它的数组决定（数组类型又由
				// 最外层五个字段之一决定）。
				kind = stack[n-1].elem
			}
			stack = append(stack, frame{
				isObj: true, kind: kind, expectKey: true,
				rawKeys: map[string]struct{}{},
				seen:    map[string]string{},
			})
		case '[':
			elem := objUnknown
			if n := len(stack); n > 0 {
				if p := &stack[n-1]; p.isObj {
					if p.kind == objFile {
						elem = fileKeyObjectKind(p.pendingKey)
					}
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
				f.pendingCanonical = ""
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
				// 课程对象结束时若发现过显式 "open": null，在这里统一报错：
				// 此时无论 id 写在 open 之前还是之后，编号都已读到。
				if closed := stack[len(stack)-1]; closed.isObj && closed.nullOpen {
					return &nullCourseOpenError{courseID: closed.courseID}
				}
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
				canonical := ""
				if c, known := matchRecordField(f.kind, t); known {
					if prev, conflict := f.seen[c]; conflict {
						return &duplicateFieldError{
							field:  c,
							first:  prev,
							second: t,
						}
					}
					f.seen[c] = t
					canonical = c
				}
				f.expectKey = false
				f.pendingKey = t
				f.pendingCanonical = canonical
				continue
			}
			// 字符串值：在课程对象内记下 id 字段的编号原文。open 写在 id
			// 之前时，对象结束处的报错仍能用编号点名；id 缺失则保持空串。
			if n > 0 {
				if f := &stack[n-1]; f.isObj && f.kind == objCourse &&
					f.pendingCanonical == "id" {
					f.courseID = t
				}
			}
			valueEnded()
			if depth == 0 {
				return nil // 顶层标量值
			}
		case nil:
			// 显式 JSON null 标量。只有课程对象的开放状态字段取 null 才是
			// 记录损坏：非指针 bool 会把 null 静默读成零值 false，未定状态
			// 会被当成明确停开。其他字段的 null（含最外层 null 列表）不在
			// 此判定，继续交给结构体解码按原规则处理。
			if n := len(stack); n > 0 {
				if f := &stack[n-1]; f.isObj && f.kind == objCourse &&
					f.pendingCanonical == "open" {
					f.nullOpen = true
				}
			}
			valueEnded()
			if depth == 0 {
				return nil
			}
		default:
			// number、bool 等其他标量值；true/false 是开放状态的合法取值。
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
