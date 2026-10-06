package credit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// recordVersion 是当前记录文件格式版本。
const recordVersion = 1

// fileData 是记录文件的磁盘格式。
type fileData struct {
	Version       int            `json:"version"`
	Courses       []*Course      `json:"courses"`
	Students      []*Student     `json:"students"`
	Requirements  []*Requirement `json:"requirements"`
	Enrollments   []*Enrollment  `json:"enrollments"`
	Waivers       []*Waiver      `json:"waivers"`
	NextResultSeq int            `json:"nextResultSeq"`
}

// Load 从 path 读取记录。
//
// 文件不存在时返回空记录集，existed=false，且不报错——即“从空记录开始”。
// 文件存在但无法读取（权限、I/O 错误等）或内容损坏（非法 JSON、结构矛盾、
// 引用悬空等）时返回错误，调用方绝不应把该文件当作空记录覆盖写入。
// 空文件同样视为损坏，而不是空记录。
//
// 整份文件必须只含一条完整记录：记录前后允许空白（空格、制表符、换行、
// 回车），但完整记录结束后只要还有任何内容——多出的右花括号或右方括号、
// 拼接的第二段 JSON、普通文字或没写完的 JSON 片段——都判为损坏并拒绝读取，
// 绝不依据已读到的前半条记录继续办理业务。
//
// 同一个 JSON 对象内的字段归属只能确定一次：出现两个完全同名的字段，或
// 两个字段名只有大小写差异却都会被读取功能识别为同一条记录字段（课程的
// credit 与 Credit、修读的 student 与 Student、最外层的 courses 与
// COURSES 等），无论两个值是否相同、顺序是否相邻，整份文件都判为损坏。
// 规则同时适用于最外层对象与其中的每条课程、学生、要求、修读、免修记录：
// 后一份即使是空数组也不能盖掉前一份。字段名以 JSON 解码后的文字比较并
// 按现有解码规则做大小写折叠（直接写出、Unicode 转义、大小写写法语义
// 等同），无法用转义加大小写的组合绕开；限制只针对同一对象，不同记录
// 各自携带同名字段是正常结构，字符串值里提到字段名不算重复。只出现一次
// 的大小写写法仍照常识别（如只有 Credit 的课程按该值读取）；不指向任何
// 记录字段的未知键不在此列，继续按未知字段拒绝。
//
// JSON 字符串的原始内容必须能还原为合法 Unicode 文字：字符串中直接写入的
// 字节不是合法 UTF-8，或 \uXXXX 转义中出现未配对的高位/低位代理项（高位
// 代理项后没有紧跟低位代理项、孤立的低位代理项、两个高位代理项相连等），
// 整份文件都判为内容损坏——标准库会把这些内容静默替换成 U+FFFD，替换后的
// 编号或依据绝不能当成正常内容继续查询、核对或登记，哪怕问题只在一条已拒绝
// 免修的依据里或本次核对不涉及的课程名称里。检查只读不写：不替用户修补转义、
// 不删字符、不改动任何申请状态。合法文字不受影响：中文、直接写入的补充平面
// 字符、完整的高低代理项转义对、转义表达的控制字符，以及用户确实写入的
// “�”（U+FFFD）都按原样使用。
//
// 有效免修的依据校验与首次正常申请一致：任何状态为有效（approved）的免修，
// 其依据必须含有实际文字；依据为空，或全部由空白字符（空格、制表符、换行、
// 回车、全角空格 U+3000、不换行空格 U+00A0 等，允许混用）组成时，整份记录
// 按内容损坏拒绝读取，即使该要求另有通过修读也不例外。已拒绝申请的依据允许
// 为空或只有空白。检查只读不写：不会补填依据、不会改动免修状态，也不会修剪
// 含实际文字的依据中原有空白。
func Load(path string) (s *Store, existed bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewStore(), false, nil
		}
		return nil, false, fmt.Errorf("无法读取记录文件 %s：%w", path, err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("无法读取记录文件 %s 的信息：%w", path, err)
	}
	if fi.Size() == 0 {
		return nil, true, fmt.Errorf("记录文件 %s 存在但为空，内容已损坏，未做任何修改", path)
	}

	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, false, fmt.Errorf("无法读取记录文件 %s：%w", path, err)
	}

	// 先按原始字节检查 JSON 字符串能否还原为合法 Unicode 文字。标准库解码
	// 会把非法 UTF-8 字节与未配对的高/低位代理项转义静默替换成 U+FFFD，
	// 替换后的学生编号、课程名称或免修依据绝不能当成正常内容继续办理业务
	// （两个本来不同的编号可能被替换成同一个），这样的文件整份判为内容
	// 损坏，哪怕问题只在一条已拒绝免修的依据里或本次核对不涉及的课程名称
	// 里。检查只读不写：不修补转义、不删字符、不改动申请状态。
	if err := scanStringUnicode(raw); err != nil {
		return nil, true, fmt.Errorf(
			"记录文件 %s 内容损坏（包含无效的 Unicode 文字：%v），未做任何修改",
			path, err)
	}

	// 先在 token 流上检查同一 JSON 对象内的字段归属。标准库直接解进结构
	// 体时会用后一个重复键静默覆盖前一个，而且结构体字段按大小写不敏感
	// 匹配 json 标签：同一门课程写了 "credit":4 与 "Credit":9（4 会被
	// 9 悄悄顶替，两个键颠倒又变成 4），最外层同时写 courses 与 COURSES
	// （后一份即使是空数组也会盖掉前一份），修读里的 student 与 Student
	// 都无法明确说明字段归属。这样的记录必须整份判为损坏，绝不能据此
	// 核对或办理——即使冲突只在本次查询没有涉及的课程或免修历史里。
	// 仅扫描第一份完整值，其后内容仍由下方的“完整记录之后不得有多余
	// 内容”检查处理。
	var dupErr *duplicateFieldError
	if err := scanDuplicateKeys(bytes.NewReader(raw)); errors.As(err, &dupErr) {
		if dupErr.first == dupErr.second {
			return nil, true, fmt.Errorf(
				"记录文件 %s 内容损坏（同一 JSON 对象内字段 %q 重复出现，字段归属无法确定），未做任何修改",
				path, dupErr.field)
		}
		return nil, true, fmt.Errorf(
			"记录文件 %s 内容损坏（同一 JSON 对象内字段 %q 与 %q 仅大小写不同，读取时都指向同一记录字段 %q，字段归属无法确定），未做任何修改",
			path, dupErr.first, dupErr.second, dupErr.field)
	}

	var data fileData
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&data); err != nil {
		return nil, true, fmt.Errorf("记录文件 %s 内容损坏（JSON 解析失败：%v），未做任何修改", path, err)
	}
	// 完整记录之后必须只剩下空白：再解一条原始 JSON，仅 io.EOF 表示记录后
	// 别无他物。不能用 dec.More() 判断——它只向前看“值起始”标记，记录后
	// 多出的右花括号/右方括号会让 More() 直接返回 false 而被漏掉。再次
	// Decode 能覆盖全部情形：成功读到值说明后面拼接了第二段 JSON；返回
	// 语法错误或 unexpected EOF 说明尾部有孤立括号、普通文字或未写完的
	// 片段。这些一律视为文件损坏。
	var extra json.RawMessage
	switch err := dec.Decode(&extra); {
	case errors.Is(err, io.EOF):
		// 唯一一份完整记录之后只有空白，符合格式。
	case err == nil:
		return nil, true, fmt.Errorf("记录文件 %s 内容损坏（完整记录之后还拼接了其他 JSON 数据），未做任何修改", path)
	default:
		return nil, true, fmt.Errorf("记录文件 %s 内容损坏（完整记录之后仍有无法解析的多余内容），未做任何修改", path)
	}

	s = NewStore()
	if err := s.loadData(&data); err != nil {
		return nil, true, fmt.Errorf("记录文件 %s 内容损坏（%v），未做任何修改", path, err)
	}
	return s, true, nil
}

// loadData 校验磁盘数据并重建索引；任何矛盾都作为损坏报错。
func (s *Store) loadData(d *fileData) error {
	if d.Version != recordVersion {
		return fmt.Errorf("不支持的记录版本 %d（当前支持版本 %d）", d.Version, recordVersion)
	}

	// 课程
	for _, c := range d.Courses {
		if c == nil {
			return errors.New("存在空的课程记录")
		}
		if c.ID == "" {
			return errors.New("存在没有编号的课程")
		}
		if c.Name == "" {
			return fmt.Errorf("课程 %s 没有名称", c.ID)
		}
		if c.Credit <= 0 {
			return fmt.Errorf("课程 %s 学分不是正整数：%d", c.ID, c.Credit)
		}
		if _, dup := s.courseByID[c.ID]; dup {
			return fmt.Errorf("课程编号 %s 重复", c.ID)
		}
		s.courses = append(s.courses, c)
		s.courseByID[c.ID] = c
	}

	// 学生
	for _, st := range d.Students {
		if st == nil || st.ID == "" {
			return errors.New("存在没有编号的学生记录")
		}
		if _, dup := s.studentByID[st.ID]; dup {
			return fmt.Errorf("学生编号 %s 重复", st.ID)
		}
		s.students = append(s.students, st)
		s.studentByID[st.ID] = st
	}

	// 要求
	for _, r := range d.Requirements {
		if r == nil {
			return errors.New("存在空的课程要求记录")
		}
		if r.StudentID == "" || r.ID == "" {
			return errors.New("存在缺少学生编号或要求编号的课程要求")
		}
		if _, ok := s.studentByID[r.StudentID]; !ok {
			return fmt.Errorf("要求 %s 引用了不存在的学生 %s", r.ID, r.StudentID)
		}
		if _, ok := s.courseByID[r.CourseID]; !ok {
			return fmt.Errorf("学生 %s 的要求 %s 引用了不存在的课程 %s",
				r.StudentID, r.ID, r.CourseID)
		}
		key := reqKey(r.StudentID, r.ID)
		if _, dup := s.reqByKey[key]; dup {
			return fmt.Errorf("学生 %s 的要求编号 %s 重复", r.StudentID, r.ID)
		}
		dk := dupKey(r.StudentID, r.CourseID)
		if other, dup := s.reqByDup[dk]; dup {
			return fmt.Errorf("学生 %s 就课程 %s 存在重复要求 %s 与 %s",
				r.StudentID, r.CourseID, other, r.ID)
		}
		s.requirements = append(s.requirements, r)
		s.reqByKey[key] = r
		s.reqByDup[dk] = r.ID
	}

	// 修读
	usedSeq := map[int]bool{}
	for _, e := range d.Enrollments {
		if e == nil {
			return errors.New("存在空的修读记录")
		}
		if e.StudentID == "" || e.ID == "" {
			return errors.New("存在缺少学生编号或修读编号的修读记录")
		}
		if _, ok := s.studentByID[e.StudentID]; !ok {
			return fmt.Errorf("修读 %s 引用了不存在的学生 %s", e.ID, e.StudentID)
		}
		r := s.reqByKey[reqKey(e.StudentID, e.ReqID)]
		if r == nil {
			return fmt.Errorf("学生 %s 的修读 %s 引用了不存在的要求 %s",
				e.StudentID, e.ID, e.ReqID)
		}
		if e.Term == "" {
			return fmt.Errorf("学生 %s 的修读 %s 缺少学期", e.StudentID, e.ID)
		}
		switch e.Result {
		case Enrolled, Passed, Failed:
		default:
			return fmt.Errorf("学生 %s 的修读 %s 含非法结果 %q", e.StudentID, e.ID, e.Result)
		}
		if e.Result == Enrolled {
			if e.ResultSeq != 0 {
				return fmt.Errorf("修读 %s 尚未提交结果却带有结果序号", e.ID)
			}
		} else {
			if e.ResultSeq <= 0 {
				return fmt.Errorf("修读 %s 已提交结果但缺少结果序号", e.ID)
			}
			if usedSeq[e.ResultSeq] {
				return fmt.Errorf("结果提交序号 %d 重复，记录已损坏", e.ResultSeq)
			}
			usedSeq[e.ResultSeq] = true
		}
		key := reqKey(e.StudentID, e.ID)
		if _, dup := s.enrByKey[key]; dup {
			return fmt.Errorf("学生 %s 的修读编号 %s 重复", e.StudentID, e.ID)
		}
		s.enrollments = append(s.enrollments, e)
		s.enrByKey[key] = e
	}

	// 免修（被拒绝的申请允许指向当时不存在的要求、依据也可以为空；
	// 已撤销的申请则必须保留一份曾经有效的申请所必需的信息）
	approvedReq := map[ownerKey]string{} // (student, req) -> waiverID
	for _, w := range d.Waivers {
		if w == nil {
			return errors.New("存在空的免修记录")
		}
		if w.StudentID == "" || w.ID == "" {
			return errors.New("存在缺少学生编号或免修编号的免修记录")
		}
		if _, ok := s.studentByID[w.StudentID]; !ok {
			return fmt.Errorf("免修 %s 引用了不存在的学生 %s", w.ID, w.StudentID)
		}
		switch w.Status {
		case WaiverApproved, WaiverRejected, WaiverRevoked:
		default:
			return fmt.Errorf("学生 %s 的免修 %s 含非法状态 %q", w.StudentID, w.ID, w.Status)
		}
		switch w.Status {
		case WaiverApproved:
			// 有效免修的依据校验与首次正常申请完全一致：依据必须含有实际
			// 文字，空串或全部由空白字符（空格、制表符、换行、全角空格
			// U+3000、不换行空格 U+00A0 等，含混用）组成都按内容损坏
			// 拒绝整份记录——这样的“有效免修”无法有据可查，即使该要求
			// 另有通过修读也不能放过。检查只读不写，绝不修剪或改写文件
			// 中已保存的依据原文，也不把它降级成已拒绝/已撤销。
			if blankBasis(w.Basis) {
				return fmt.Errorf("学生 %s 的有效免修 %s 缺少依据（依据为空或全部为空白字符）",
					w.StudentID, w.ID)
			}
			rk := reqKey(w.StudentID, w.ReqID)
			if s.reqByKey[rk] == nil {
				return fmt.Errorf("有效免修 %s 指向的要求 %s 不存在或不属于该学生", w.ID, w.ReqID)
			}
			if other, dup := approvedReq[rk]; dup {
				return fmt.Errorf("学生 %s 的要求 %s 同时存在有效免修 %s 与 %s",
					w.StudentID, w.ReqID, other, w.ID)
			}
			approvedReq[rk] = w.ID
		case WaiverRevoked:
			// 撤销只取消免修对课程要求的满足作用，不能把缺少原依据、
			// 失去要求归属的记录变成合法历史：目标要求必须真实存在于
			// 该免修所属学生名下（同号要求只在其他学生名下不算），
			// 原依据必须含有非空白内容（空白口径与有效免修一致，全角
			// 空格、不换行空格等同样不算实际依据）。检查只读不写，
			// 依据中有实际文字时原有空白一律保留，绝不为了通过检查
			// 改写保存下来的材料内容。失效历史不参与有效免修的唯一
			// 性限制。
			if s.reqByKey[reqKey(w.StudentID, w.ReqID)] == nil {
				return fmt.Errorf("学生 %s 的已撤销免修 %s 目标要求无效（要求 %s 不存在于该学生名下）",
					w.StudentID, w.ID, w.ReqID)
			}
			if blankBasis(w.Basis) {
				return fmt.Errorf("学生 %s 的已撤销免修 %s 原依据为空", w.StudentID, w.ID)
			}
		}
		key := reqKey(w.StudentID, w.ID)
		if _, dup := s.waiverByKey[key]; dup {
			return fmt.Errorf("学生 %s 的免修编号 %s 重复", w.StudentID, w.ID)
		}
		s.waivers = append(s.waivers, w)
		s.waiverByKey[key] = w
	}

	if d.NextResultSeq < 0 {
		return errors.New("结果提交序号计数器为负数")
	}
	for seq := range usedSeq {
		if seq > d.NextResultSeq {
			return fmt.Errorf("结果提交序号计数器 %d 小于已有序号 %d", d.NextResultSeq, seq)
		}
	}
	s.nextResultSeq = d.NextResultSeq
	return nil
}

// invalidTextError 表示待保存记录中含有无法原样写入文件的文字：某个字符串
// 字段不是合法 UTF-8。category 指出记录类别（课程、学生、课程要求、修读、
// 免修），id 是该条记录的编号（仅用于定位，编号本身也可能就是问题所在，
// 按原始字节呈现），field 指出具体字段。
type invalidTextError struct {
	category string
	id       string
	field    string
}

func (e *invalidTextError) Error() string {
	return fmt.Sprintf(
		"%s记录（编号 %s）的字段“%s”含无法原样保存的非法 UTF-8 文字",
		e.category, e.id, e.field)
}

// validateSaveText 检查全部待写入记录的字符串字段是否都是合法 UTF-8。
//
// 背景：encoding/json 在序列化时会把字符串里的非法 UTF-8 字节静默替换成
// U+FFFD（“�”）却照常返回成功。若直接保存，免修依据会被改写，两个本来
// 不同的学生编号还可能被替换成同一个编号，让原本可读的记录文件变成编号
// 重复的损坏文件，而命令却提示成功。因此凡是要写入文件的文字——课程、
// 学生、课程要求、修读、免修的编号、名称、学期、依据、原因等——都必须先
// 通过本检查，任何一个字段不合法就拒绝整次保存。
//
// 检查只读不补：绝不替换、清空或修补任何原始字符串，也不删除出问题的
// 那条历史（即使它只是一条已拒绝或已撤销的免修），更不会跳过该字段只存
// 其他记录。合法文字不受影响：中文、补充平面字符、用户确实输入的“�”
// （U+FFFD）都是合法 UTF-8；编号中合法的控制字符与免修依据中原有的空白
// 也原样保留。命令行里字面出现的“\uD800”只是反斜线加普通字母数字，是
// 合法 ASCII 文字，不按记录文件中的 Unicode 转义处理。
func (s *Store) validateSaveText() error {
	for _, c := range s.courses {
		if c == nil {
			continue
		}
		if err := checkSaveText("课程", c.ID, "编号", c.ID); err != nil {
			return err
		}
		if err := checkSaveText("课程", c.ID, "名称", c.Name); err != nil {
			return err
		}
	}
	for _, st := range s.students {
		if st == nil {
			continue
		}
		if err := checkSaveText("学生", st.ID, "编号", st.ID); err != nil {
			return err
		}
	}
	for _, r := range s.requirements {
		if r == nil {
			continue
		}
		if err := checkSaveText("课程要求", r.ID, "编号", r.ID); err != nil {
			return err
		}
		if err := checkSaveText("课程要求", r.ID, "学生编号", r.StudentID); err != nil {
			return err
		}
		if err := checkSaveText("课程要求", r.ID, "课程编号", r.CourseID); err != nil {
			return err
		}
	}
	for _, e := range s.enrollments {
		if e == nil {
			continue
		}
		if err := checkSaveText("修读", e.ID, "编号", e.ID); err != nil {
			return err
		}
		if err := checkSaveText("修读", e.ID, "学生编号", e.StudentID); err != nil {
			return err
		}
		if err := checkSaveText("修读", e.ID, "要求编号", e.ReqID); err != nil {
			return err
		}
		if err := checkSaveText("修读", e.ID, "学期", e.Term); err != nil {
			return err
		}
		if err := checkSaveText("修读", e.ID, "结果", string(e.Result)); err != nil {
			return err
		}
	}
	for _, w := range s.waivers {
		if w == nil {
			continue
		}
		if err := checkSaveText("免修", w.ID, "编号", w.ID); err != nil {
			return err
		}
		if err := checkSaveText("免修", w.ID, "学生编号", w.StudentID); err != nil {
			return err
		}
		if err := checkSaveText("免修", w.ID, "要求编号", w.ReqID); err != nil {
			return err
		}
		if err := checkSaveText("免修", w.ID, "依据", w.Basis); err != nil {
			return err
		}
		if err := checkSaveText("免修", w.ID, "状态", string(w.Status)); err != nil {
			return err
		}
		if err := checkSaveText("免修", w.ID, "原因", w.Reason); err != nil {
			return err
		}
	}
	return nil
}

// checkSaveText 校验单个待保存字段的文字是否为合法 UTF-8。
func checkSaveText(category, id, field, value string) error {
	if !utf8.ValidString(value) {
		return &invalidTextError{category: category, id: id, field: field}
	}
	return nil
}

// Save 将全部记录原子写入 path：先写同目录临时文件，再重命名覆盖，
// 避免写到一半时损坏原文件。
//
// 写入前先校验全部待保存字符串都是合法 UTF-8：标准库序列化会把非法字节
// 静默替换成 U+FFFD，因此含非法文字的数据绝不能进入序列化与临时文件阶段。
// 校验失败时整次保存直接返回错误——不创建/不改动目标文件（目标原本不
// 存在时也不会留下新记录文件或临时文件），不替换、清空或修补待保存记录
// 中的任何原始字符串，即使问题只出现在一条已拒绝或已撤销免修的字段里。
func (s *Store) Save(path string) (err error) {
	if err := s.validateSaveText(); err != nil {
		return fmt.Errorf("记录中存在无法原样保存的文字，拒绝写入 %s：%w", path, err)
	}
	data := fileData{
		Version:       recordVersion,
		Courses:       s.courses,
		Students:      s.students,
		Requirements:  s.requirements,
		Enrollments:   s.enrollments,
		Waivers:       s.waivers,
		NextResultSeq: s.nextResultSeq,
	}
	buf, err := json.MarshalIndent(&data, "", "  ")
	if err != nil {
		return fmt.Errorf("无法序列化记录：%w", err)
	}
	buf = append(buf, '\n')

	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	tmp, err := os.CreateTemp(dir, ".credit-*.tmp")
	if err != nil {
		return fmt.Errorf("无法在 %s 创建临时记录文件：%w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	if _, err = tmp.Write(buf); err != nil {
		return fmt.Errorf("写入记录文件失败：%w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("写入记录文件失败：%w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("写入记录文件失败：%w", err)
	}
	if err = os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("设置记录文件权限失败：%w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("保存记录文件 %s 失败：%w", path, err)
	}
	return nil
}
