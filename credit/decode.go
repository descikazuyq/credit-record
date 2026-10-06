package credit

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// fieldConflictError 表示同一个 JSON 对象内出现了两个会被现有读取功能
// 识别为同一个记录字段的字段名：既包括逐字相同的重复键，也包括仅大小写
// 不同、但解码时会写入同一结构体字段的两个键（例如 "credit" 与 "Credit"）。
type fieldConflictError struct {
	// field 是参与冲突的字段名之一（按 JSON 解码后的实际文字报告）。
	field string
}

func (e *fieldConflictError) Error() string {
	return fmt.Sprintf("同一 JSON 对象内字段名 %q 重复出现，字段归属无法确定", e.field)
}

// fieldKeysByObject 给出各类记录对象中“会被读取功能识别为同一记录字段”的
// 字段名集合，统一取 JSON 字段名的小写形式（即 strings.ToLower(json tag)，
// 因此 nextResultSeq 写作 nextresultseq）：
//   - true 这一组是文件最外层对象（fileData）的字段；
//   - false 这一组是数组内单条记录（课程/学生/要求/修读/免修）的字段合集。
//
// 规则：在同一对象内，只要有两个字段名经 Unicode 反转义、并按
// strings.ToLower 归并后落在同一个集合元素上，就算冲突——无论它们逐字
// 是否相同、顺序与位置如何、两个值是否一致。这样 "credit" 与 "Credit"
// 不能再让后一个值悄悄顶替前一个（学分 4 被换成 9、学生归属或免修状态被
// 改写）。仅出现一次的大小写写法（例如只有 "Credit"）仍照常被标准库识别，
// 单字段兼容性不变；不在任何集合内的键（未知字段）由后续的
// DisallowUnknownFields 阶段拒绝，本扫描不据此做大小写归并。
//
// 各记录结构体的字段名彼此互不重叠（id/name/credit/open、student/course、
// req/term/result/resultSeq、basis/status/reason 等），且冲突判定只限同一
// 对象，因此把五类记录的字段合成一组不会把“不同对象里的不同字段”误判为
// 同一字段——不同课程、不同学生等各自对象独立携带各自的字段，互不比较。
var fieldKeysByObject = map[bool][]string{
	// 最外层 fileData。
	true: {
		"version", "courses", "students", "requirements",
		"enrollments", "waivers", "nextresultseq",
	},
	// 数组内的单条记录（课程/学生/要求/修读/免修）。这些字段名合在一个
	// 集合里使用：只有同一个对象内同时出现两个键才会冲突，不同记录对象
	// 各自携带同名字段互不比较；字段名集合互不重叠，因此把五类记录的键
	// 合并不会把“不同字段”误判为同一字段。
	false: {
		// Course
		"id", "name", "credit", "open",
		// Student
		// （id 已列）
		// Requirement
		"student", "course",
		// Enrollment
		"req", "term", "result", "resultseq",
		// Waiver
		"basis", "status", "reason",
	},
}

// recognizedFieldKey 返回键名 key（JSON 解码后的实际文字）在给定对象类型
// 所识别字段集合中的小写归并形式；该对象不识别此键时返回 ""（未知字段）。
func recognizedFieldKey(isTop bool, key string) string {
	lower := strings.ToLower(key)
	for _, k := range fieldKeysByObject[isTop] {
		if lower == k {
			return k
		}
	}
	return ""
}

// scanDuplicateKeys 扫描第一份完整 JSON 值的 token 流，在每个对象内检查
// 字段归属唯一；第一份值之后的字节一律不读（调用方另行检查完整记录之后
// 是否还拼接了多余内容，那里的报错信息需要与本检查区分开）。
//
// 背景：encoding/json 直接解进结构体（或 map）时，同一对象内两个会映射到
// 同一字段的键会被后一个静默覆盖，读不出“曾经写过两个”。逐字同名如此，
// 仅大小写不同而指向同一结构体字段的两个键同样如此（例如课程同时写出
// "credit":4 与 "Credit":9，先写谁就用谁）。因此在正常结构体解码之前先
// 单独扫描一遍 token 流。
//
// 规则：
//   - 同一个 JSON 对象内，只要有两个字段名被现有读取功能识别为同一个记录
//     字段，就返回 *fieldConflictError（含字段名），整份文件随后按内容
//     损坏拒绝：逐字重复、仅大小写不同（credit/Credit、courses/COURSES、
//     student/Student）都算；字段是否相邻、出现顺序、两个值是否相同都不
//     影响判定。这一规则适用于最外层对象和数组中的每条课程、学生、要求、
//     修读、免修记录。
//   - 比较以 JSON 解码后的文字为准：Token() 返回的键名已完成 Unicode
//     反转义，所以直接写出与 \uXXXX 转义形式、以及“转义 + 大小写”的组合
//     （"credit" 与 "Credit"）仍按解码后实际所指字段判断，无法借
//     转义绕开；大小写归并用 strings.EqualFold 口径（strings.ToLower），
//     与标准库结构体解码匹配字段名的方式一致。
//   - 只出现一次的大小写写法不是冲突：仅有 "Credit" 的课程仍按该值读取。
//     限制只针对同一对象：对象与数组可任意嵌套，数组各元素、不同课程或
//     不同学生等不同对象各自携带同名字段是正常结构，互不比较。
//   - 字符串“值”里出现字段名（依据、课程名称等）不是键，不参与检查。
//     未知字段的大小写变体不在这里报错（交给后续 DisallowUnknownFields
//     阶段拒绝），但同一对象内两个逐字相同的未知键仍按同名规则判损坏。
func scanDuplicateKeys(r io.Reader) error {
	type frame struct {
		isObj bool
		// isTop 仅对对象有意义：标记文件最外层对象。
		isTop bool
		// expectKey 仅对对象有意义：true 表示下一个字符串 token 是键
		// （或直接遇到 '}'）；false 表示正在等待该键所对应的值。
		// 对象内 token 天然按键、值、键、值交替（逗号不产生 token），
		// 借此区分字符串 token 是键还是普通字符串值。
		expectKey bool
		// raw 逐字记录本对象内出现过的全部键名（JSON 解码后的实际文字），
		// 用于保留“完全同名字段即损坏”的既有规则，未知字段也不例外。
		raw map[string]struct{}
		// seen 记录本对象内已经出现过的“识别字段归并名 -> 该字段首次
		// 出现的实际键名”，用于把仅大小写不同、但会写入同一记录字段的
		// 两个键判为冲突。未知字段不记录，交由结构体解码阶段处理。
		seen map[string]string
	}
	var stack []frame
	depth := 0

	push := func(isObj, isTop bool) {
		f := frame{isObj: isObj, isTop: isTop}
		if isObj {
			f.expectKey = true
			f.raw = map[string]struct{}{}
			f.seen = map[string]string{}
		}
		stack = append(stack, f)
	}
	// valueEnded 通知栈顶对象：刚读完一个值（标量或嵌套结构的结束括号）。
	valueEnded := func() {
		if n := len(stack); n > 0 {
			f := &stack[n-1]
			if f.isObj && !f.expectKey {
				f.expectKey = true
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
			case '{':
				depth++
				// 开在最外层（栈尚为空）的对象就是文件最外层对象；其余
				// 对象都是某条记录或嵌套结构，按具体记录字段集合校验。
				push(true, len(stack) == 0)
			case '[':
				depth++
				push(false, false)
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
				// 对象键：Token() 已按解码后的文字返回。完全同名的键先按
				// 既有规则判冲突——即使是读取功能不认识的未知字段，同一对象
				// 内写两个逐字相同的键也无法说明归属，仍判损坏。
				if _, dup := f.raw[t]; dup {
					return &fieldConflictError{field: t}
				}
				f.raw[t] = struct{}{}
				// 再确认它是该对象会识别的记录字段，并在同一对象内按大小
				// 写归并判冲突：credit 与 Credit、courses 与 COURSES 等会
				// 被解码为同一字段的两个键，同样不能让后者顶替前者。
				if canon := recognizedFieldKey(f.isTop, t); canon != "" {
					if first, dup := f.seen[canon]; dup {
						// 报告先出现的那个实际字段名：无论第二个键写法
						// （大小写/转义）如何，都能据此定位冲突字段。
						return &fieldConflictError{field: first}
					}
					f.seen[canon] = t
				}
				f.expectKey = false
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
