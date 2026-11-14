package service

import (
	"html"
	"regexp"
	"strings"

	xhtml "golang.org/x/net/html"
)

// massEmailAllowedTags 富文本正文允许保留的标签白名单。
var massEmailAllowedTags = map[string]struct{}{
	"a": {}, "b": {}, "blockquote": {}, "br": {}, "code": {}, "div": {},
	"em": {}, "h1": {}, "h2": {}, "h3": {}, "h4": {}, "h5": {}, "h6": {},
	"hr": {}, "i": {}, "img": {}, "li": {}, "ol": {}, "p": {}, "pre": {},
	"s": {}, "small": {}, "span": {}, "strong": {}, "table": {}, "tbody": {},
	"td": {}, "th": {}, "thead": {}, "tr": {}, "u": {}, "ul": {},
}

// massEmailVoidTags 无需闭合标签（白名单内）。
var massEmailVoidTags = map[string]struct{}{
	"br": {}, "hr": {}, "img": {},
}

// massEmailVoidHTMLTags 是 HTML 规范里的全部 void 元素。
//
// 它们没有闭合标签：如果按"丢弃并进入子内容"处理（dropDepth++），dropDepth 永远
// 减不回来，会把后面所有正文都吞掉（例如 <input> 之后的 <p>after</p>，或者文档
// <head><meta charset></head> 之后的 <body>）。所以 void 元素只丢自己，不加深度。
var massEmailVoidHTMLTags = map[string]struct{}{
	"area": {}, "base": {}, "br": {}, "col": {}, "embed": {}, "hr": {},
	"img": {}, "input": {}, "link": {}, "meta": {}, "param": {},
	"source": {}, "track": {}, "wbr": {},
}

// massEmailDropWithContent 这些标签连同内容一起丢弃。
var massEmailDropWithContent = map[string]struct{}{
	"script": {}, "style": {}, "iframe": {}, "object": {}, "embed": {},
	"noscript": {}, "template": {}, "svg": {}, "math": {}, "form": {},
	"input": {}, "button": {}, "textarea": {}, "select": {}, "link": {},
	"meta": {}, "title": {}, "head": {},
}

// massEmailAllowedAttrs 每个标签允许保留的属性。
var massEmailAllowedAttrs = map[string]map[string]struct{}{
	"a":   {"href": {}, "title": {}, "target": {}, "rel": {}},
	"img": {"src": {}, "alt": {}, "width": {}, "height": {}},
	"td":  {"colspan": {}, "rowspan": {}},
	"th":  {"colspan": {}, "rowspan": {}},
	"*":   {"style": {}},
}

// massEmailAllowedStyleProps 内联样式白名单（tiptap 常用属性）。
var massEmailAllowedStyleProps = map[string]struct{}{
	"color": {}, "background-color": {}, "text-align": {}, "font-weight": {},
	"font-style": {}, "text-decoration": {}, "font-size": {}, "line-height": {},
	"margin": {}, "margin-left": {}, "margin-right": {}, "padding": {},
	"padding-left": {}, "padding-right": {}, "border-collapse": {},
}

// massEmailDangerousStyleValue 命中即丢弃整条样式声明。
var massEmailDangerousStyleValue = []string{
	"expression(", "javascript:", "vbscript:", "url(", "@import", "behavior:", "binding:",
}

// SanitizeMassEmailHTML 服务端净化富文本 HTML。
//
// 采用白名单策略：只保留排版类标签与少量属性，丢弃 script/style/iframe 等
// 可执行或可外链的节点（连同内容），URL 只允许 http/https/mailto/data:image。
// 输出始终保持 well-formed，便于邮件客户端渲染。
func SanitizeMassEmailHTML(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}

	tokenizer := xhtml.NewTokenizer(strings.NewReader(trimmed))
	var out strings.Builder
	// dropDepth 记录当前处于"丢弃内容"的嵌套深度。
	dropDepth := 0

	for {
		switch tokenizer.Next() {
		case xhtml.ErrorToken:
			return strings.TrimSpace(out.String())
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			token := tokenizer.Token()
			name := strings.ToLower(token.Data)
			selfClosing := token.Type == xhtml.SelfClosingTagToken

			if _, drop := massEmailDropWithContent[name]; drop {
				_, void := massEmailVoidHTMLTags[name]
				if !selfClosing && !void {
					dropDepth++
				}
				continue
			}
			if dropDepth > 0 {
				continue
			}
			if _, ok := massEmailAllowedTags[name]; !ok {
				continue
			}

			attrs := sanitizeMassEmailAttrs(name, token.Attr)
			if _, void := massEmailVoidTags[name]; void || selfClosing {
				_, _ = out.WriteString("<" + name + attrs + " />")
				continue
			}
			_, _ = out.WriteString("<" + name + attrs + ">")

		case xhtml.EndTagToken:
			name := strings.ToLower(tokenizer.Token().Data)
			if _, drop := massEmailDropWithContent[name]; drop {
				if dropDepth > 0 {
					dropDepth--
				}
				continue
			}
			if dropDepth > 0 {
				continue
			}
			if _, ok := massEmailAllowedTags[name]; !ok {
				continue
			}
			if _, void := massEmailVoidTags[name]; void {
				continue
			}
			_, _ = out.WriteString("</" + name + ">")

		case xhtml.TextToken:
			if dropDepth > 0 {
				continue
			}
			_, _ = out.WriteString(html.EscapeString(tokenizer.Token().Data))
		}
	}
}

// sanitizeMassEmailAttrs 过滤属性，返回已拼好的 ` k="v"` 片段。
func sanitizeMassEmailAttrs(tag string, attrs []xhtml.Attribute) string {
	if len(attrs) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, attr := range attrs {
		name := strings.ToLower(attr.Key)
		if strings.HasPrefix(name, "on") {
			continue // 事件处理器一律丢弃
		}
		if !massEmailAttrAllowed(tag, name) {
			continue
		}

		value := strings.TrimSpace(attr.Val)
		switch name {
		case "href", "src":
			if !massEmailSafeURL(value) {
				continue
			}
		case "style":
			value = sanitizeMassEmailStyle(value)
			if value == "" {
				continue
			}
		}
		_, _ = builder.WriteString(" " + name + `="` + html.EscapeString(value) + `"`)
	}
	return builder.String()
}

func massEmailAttrAllowed(tag, attr string) bool {
	if allowed, ok := massEmailAllowedAttrs[tag]; ok {
		if _, ok := allowed[attr]; ok {
			return true
		}
	}
	if allowed, ok := massEmailAllowedAttrs["*"]; ok {
		if _, ok := allowed[attr]; ok {
			return true
		}
	}
	return false
}

// massEmailSafeURL 只放行 http/https/mailto/tel 与内嵌图片 data URL。
func massEmailSafeURL(raw string) bool {
	lowered := strings.ToLower(strings.TrimSpace(raw))
	if lowered == "" {
		return false
	}
	// 去掉空白与控制字符后再判断协议，避免 "java\tscript:" 之类的绕过。
	normalized := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, lowered)

	for _, prefix := range []string{"http://", "https://", "mailto:", "tel:"} {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return strings.HasPrefix(normalized, "data:image/png;base64,") ||
		strings.HasPrefix(normalized, "data:image/jpeg;base64,") ||
		strings.HasPrefix(normalized, "data:image/gif;base64,") ||
		strings.HasPrefix(normalized, "data:image/webp;base64,")
}

// sanitizeMassEmailStyle 按属性白名单过滤内联样式。
func sanitizeMassEmailStyle(style string) string {
	declarations := strings.Split(style, ";")
	kept := make([]string, 0, len(declarations))
	for _, declaration := range declarations {
		parts := strings.SplitN(declaration, ":", 2)
		if len(parts) != 2 {
			continue
		}
		prop := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		if prop == "" || value == "" {
			continue
		}
		if _, ok := massEmailAllowedStyleProps[prop]; !ok {
			continue
		}
		lowered := strings.ToLower(value)
		dangerous := false
		for _, needle := range massEmailDangerousStyleValue {
			if strings.Contains(lowered, needle) {
				dangerous = true
				break
			}
		}
		if dangerous {
			continue
		}
		kept = append(kept, prop+": "+value)
	}
	return strings.Join(kept, "; ")
}

// massEmailVariablePattern 匹配 {{ name }} / {{name}} 形式的变量占位符。
var massEmailVariablePattern = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)

// RenderMassEmailTemplate 渲染正文变量。
//
// 支持 {{name}} {{email}} {{enterprise_name}} {{site_name}}（大小写敏感，允许内部空白）。
// 变量值做 HTML 转义后写入，未知变量原样保留，便于管理员发现拼写错误。
func RenderMassEmailTemplate(template string, vars map[string]string) string {
	if template == "" {
		return ""
	}
	return massEmailVariablePattern.ReplaceAllStringFunc(template, func(match string) string {
		key := strings.ToLower(massEmailVariablePattern.FindStringSubmatch(match)[1])
		value, ok := vars[key]
		if !ok {
			return match
		}
		return html.EscapeString(value)
	})
}

// RenderMassEmailTemplatePlain 纯文本场景（邮件主题）的变量渲染：不做 HTML 转义。
func RenderMassEmailTemplatePlain(template string, vars map[string]string) string {
	if template == "" {
		return ""
	}
	return massEmailVariablePattern.ReplaceAllStringFunc(template, func(match string) string {
		key := strings.ToLower(massEmailVariablePattern.FindStringSubmatch(match)[1])
		value, ok := vars[key]
		if !ok {
			return match
		}
		return value
	})
}

// MassEmailTemplateVariables 返回正文支持的变量名（用于前端提示与校验）。
func MassEmailTemplateVariables() []string {
	return []string{"name", "email", "enterprise_name", "site_name"}
}

// InjectMassEmailTrackingPixel 在正文末尾注入 1x1 追踪像素。
//
// 优先插到 </body> 之前，没有 body 就追加到末尾。
func InjectMassEmailTrackingPixel(bodyHTML, pixelURL string) string {
	if strings.TrimSpace(pixelURL) == "" {
		return bodyHTML
	}
	pixel := `<img src="` + html.EscapeString(pixelURL) + `" width="1" height="1" alt="" style="display:block;width:1px;height:1px;border:0;outline:none;" />`

	lowered := strings.ToLower(bodyHTML)
	if idx := strings.LastIndex(lowered, "</body>"); idx >= 0 {
		return bodyHTML[:idx] + pixel + bodyHTML[idx:]
	}
	return bodyHTML + pixel
}
