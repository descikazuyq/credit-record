package credit

import (
	"encoding/json"
	"fmt"
	"io"
)

// duplicateFieldError 表示同一个 JSON 对象内出现了重复字段名。
type duplicateFieldError struct {
	field string
}

func (e *duplicateFieldError) Error() string {
	return fmt.Sprintf("同一 JSON 对象内字段名 %q 重复出现，字段归属无法确定", e.field)
}

// scanDuplicateKeys 扫描第一份完整 JSON 值的 token 流，在每个对象内检查
// 字段名唯一；第一份值之后的字节一律不读（调用方另行检查完整记录之后是否
// 还拼接了多余内容，那里的报错信息需要与本检查区分开）。
//
// 背景：encoding/json 直接解进结构体（或 map）时，同一对象内的重复键会被
// 后一个静默覆盖，读不出“曾经写过两个”，因此在正常结构体解码之前先单独
// 扫描一遍 token 流。
//
// 规则：
//   - 同一个 JSON 对象内字段名只能出现一次；重复字段是否相邻、两个值是否
//     相同都不影响判定，发现即返回 *duplicateFieldError（含字段名）。
//   - 比较以 JSON 解码后的文字为准：Token() 返回的键名已完成 Unicode
//     反转义，所以直接写出与 \uXXXX 转义形式的同名键仍算重复。
//   - 限制只针对同一对象：对象与数组可任意嵌套，数组各元素、不同课程或
//     不同学生等不同对象各自携带同名字段是正常结构，互不比较。
//   - 字符串“值”里出现字段名（依据、课程名称等）不是键，不参与检查。
func scanDuplicateKeys(r io.Reader) error {
	type frame struct {
		isObj bool
		// expectKey 仅对对象有意义：true 表示下一个字符串 token 是键
		// （或直接遇到 '}'）；false 表示正在等待该键所对应的值。
		// 对象内 token 天然按键、值、键、值交替（逗号不产生 token），
		// 借此区分字符串 token 是键还是普通字符串值。
		expectKey bool
		keys      map[string]struct{}
	}
	var stack []frame
	depth := 0

	push := func(isObj bool) {
		f := frame{isObj: isObj}
		if isObj {
			f.expectKey = true
			f.keys = map[string]struct{}{}
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
				push(true)
			case '[':
				depth++
				push(false)
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
				// 对象键：Token() 已按解码后的文字返回。
				if _, dup := stack[n-1].keys[t]; dup {
					return &duplicateFieldError{field: t}
				}
				stack[n-1].keys[t] = struct{}{}
				stack[n-1].expectKey = false
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
