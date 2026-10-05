package credit

import (
	"fmt"
	"unicode/utf8"
)

// invalidUnicodeError 表示记录文件的 JSON 字符串原始内容无法还原为合法
// Unicode 文字：要么字符串里直接写出的字节不是合法 UTF-8，要么 \uXXXX
// 转义里出现了未配对的代理项（孤立的高/低代理项、两个高位代理项相连等）。
type invalidUnicodeError struct {
	kind   string
	offset int
}

func (e *invalidUnicodeError) Error() string {
	return fmt.Sprintf("%s（字节偏移 %d 附近）", e.kind, e.offset)
}

// scanValidUnicode 在不做任何字符替换的前提下，按原始字节扫描第一份完整
// JSON 值中出现的每一个字符串——字段名与字段值同等对待——确认其内容都能
// 还原为合法 Unicode 文字：
//
//   - 字符串中直接写出的字节必须构成合法 UTF-8；
//   - \uXXXX 转义里，高位代理项（U+D800–U+DBFF）后必须紧跟一个低位代理项
//     转义（U+DC00–U+DFFF）；单独的低位代理项、高位代理项后没有低位代理项、
//     两个高位代理项连在一起，都不是某个普通字符，一律拒绝。
//
// 背景：encoding/json 会把上述两种情形静默替换成 U+FFFD 后照常返回，
// json.Valid 也把这样的输入判为“有效”。替换会改变学生编号、课程名称与
// 免修依据的原意，绝不能拿替换后的文字继续查询、核对或登记，因此这一检查
// 必须在标准库解码之前按原始字节进行。
//
// 本函数只管 Unicode 文字这一件事，不负责 JSON 语法：遇到未结束的字符串、
// 残缺或非法转义时即结束本次扫描（放行），由调用方随后的标准 JSON 解析判
// 为语法损坏。语法合法的文件中转义必然完整，因此所有“会被标准库接受、但
// 文字已被替换”的情形在这里都逃不掉；而本身语法已损坏的文件也不会因为
// 本函数提前返回而被放过。
//
// 用户真正写入的 U+FFFD（直接写出的 UTF-8 字节 EF BF BD 或 � 转义）
// 是合法文字；直接写出的补充平面字符、完整的高低代理项对、转义表达的控制
// 字符都按原意保留，本函数既不替换也不改写。只扫描第一份完整顶层值，与
// 重复字段扫描的范围一致，完整记录之后的多余内容由调用方另行检查。
func scanValidUnicode(p []byte) error {
	i := 0
scan:
	for i < len(p) {
		switch p[i] {
		case ' ', '\t', '\r', '\n':
			i++
		default:
			break scan
		}
	}
	if i >= len(p) {
		return nil // 空输入交由后续解析报错
	}
	// 顶层标量（数字、true/false/null 或语法垃圾）内部不含字符串，直接
	// 放行：合法标量没有可检查的文字，其余情形由后续 JSON 解析拒绝。
	if c := p[i]; c != '{' && c != '[' && c != '"' {
		return nil
	}
	depth := 0
	for i < len(p) {
		switch p[i] {
		case ' ', '\t', '\r', '\n':
			i++
		case '{', '[':
			depth++
			i++
		case '}', ']':
			depth--
			i++
			if depth == 0 {
				return nil // 唯一的顶层值已完整结束
			}
		case '"':
			next, err := scanStringUnicode(p, i+1)
			if err != nil {
				return err
			}
			i = next
			if depth == 0 {
				return nil // 顶层字符串值
			}
		default:
			// 结构字符、逗号、冒号、数字与字面量都不可能含字符串文字。
			i++
		}
	}
	return nil
}

// scanStringUnicode 校验从开引号之后开始的一段 JSON 字符串原始内容，
// 返回越过闭引号后的位置。字符串未正常结束或转义残缺/非法时返回
// len(p) 且不报错——这些属于 JSON 语法错误，交由调用方的标准解析拒绝。
func scanStringUnicode(p []byte, i int) (int, error) {
	for i < len(p) {
		c := p[i]
		switch {
		case c == '"':
			return i + 1, nil
		case c == '\\':
			if i+1 >= len(p) {
				return len(p), nil // 残缺转义：语法阶段拒绝
			}
			switch p[i+1] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				i += 2
			case 'u':
				next, err := scanUnicodeEscape(p, i)
				if err != nil {
					return 0, err
				}
				if next < 0 {
					return len(p), nil // \u 转义不完整：语法阶段拒绝
				}
				i = next
			default:
				return len(p), nil // 非法转义：语法阶段拒绝
			}
		case c < 0x20:
			i++ // 未转义控制字符：语法阶段拒绝
		case c < utf8.RuneSelf:
			i++
		default:
			r, size := utf8.DecodeRune(p[i:])
			if r == utf8.RuneError && size == 1 {
				return 0, &invalidUnicodeError{
					kind:   "字符串中直接写入了不是合法 UTF-8 的字节，无法还原为 Unicode 文字",
					offset: i,
				}
			}
			i += size
		}
	}
	return len(p), nil // 字符串未结束：语法阶段拒绝
}

// scanUnicodeEscape 处理位于 p[i] 的一个 \uXXXX 转义，返回越过该转义后的
// 位置；返回负值表示转义不完整（交由语法检查）。高位代理项必须与紧随其后
// 的低位代理项转义配对，否则按无效文字报错。
func scanUnicodeEscape(p []byte, i int) (int, error) {
	r, ok := jsonHex4(p, i+2)
	if !ok {
		return -1, nil
	}
	next := i + 6
	switch {
	case r < 0xD800 || r > 0xDFFF:
		return next, nil // 普通 BMP 字符（U+FFFD 也在此列，属合法文字）
	case r >= 0xDC00:
		return 0, &invalidUnicodeError{
			kind:   fmt.Sprintf("字符串中出现未配对的低位代理项转义 \\u%04X，不能当作普通字符", r),
			offset: i,
		}
	}
	// 高位代理项：后面必须紧跟一个 \uXXXX，且其值在低位代理项区间。
	if next+1 >= len(p) || p[next] != '\\' || p[next+1] != 'u' {
		return 0, &invalidUnicodeError{
			kind: fmt.Sprintf(
				"字符串中高位代理项转义 \\u%04X 后没有紧跟对应的低位代理项转义，不能当作普通字符", r),
			offset: i,
		}
	}
	low, ok := jsonHex4(p, next+2)
	if !ok {
		return -1, nil // 后一个 \u 转义本身残缺：语法阶段拒绝
	}
	if low < 0xDC00 || low > 0xDFFF {
		return 0, &invalidUnicodeError{
			kind: fmt.Sprintf(
				"字符串中高位代理项转义 \\u%04X 后紧跟的不是低位代理项转义（得到 \\u%04X），代理项没有成对出现",
				r, low),
			offset: i,
		}
	}
	return next + 6, nil
}

// jsonHex4 读取 p[j:j+4] 的四个十六进制数位并拼成数值；不足四位或含非
// 十六进制字符时返回 ok=false。
func jsonHex4(p []byte, j int) (int, bool) {
	if j+4 > len(p) {
		return 0, false
	}
	v := 0
	for k := 0; k < 4; k++ {
		var d int
		switch c := p[j+k]; {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'f':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = int(c-'A') + 10
		default:
			return 0, false
		}
		v = v<<4 | d
	}
	return v, true
}
