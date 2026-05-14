package mail

import (
	"fmt"
	"html"
	"strings"

	"github.com/resend/resend-go/v3"

	"mechhub-back/internal/config"
)

type Sender struct {
	client  *resend.Client
	from    string
	baseURL string
	logoURL string
	bgURL   string
}

func New(cfg *config.Config) *Sender {
	return &Sender{
		client:  resend.NewClient(cfg.Mail.ResendAPIKey),
		from:    cfg.Mail.From,
		baseURL: cfg.App.BaseURL,
		logoURL: cfg.Mail.LogoURL,
		bgURL:   cfg.Mail.BgURL,
	}
}

func (s *Sender) SendVerifyEmail(to, token string) error {
	link := fmt.Sprintf("%s/verify/student?token=%s", s.baseURL, token)
	body := cardLayout(cardArgs{
		LogoURL:     s.logoURL,
		BgURL:       s.bgURL,
		Title:       "验证您的 MechHub 邮箱",
		Description: "欢迎加入 MechHub。请点击下方按钮完成邮箱验证。",
		ButtonText:  "验证邮箱",
		ButtonLink:  link,
		Footer:      "此链接有效期较短，请尽快验证。",
	})
	return s.send(to, "验证您的 MechHub 邮箱", body)
}

func (s *Sender) SendTeacherApprovalEmail(to []string, teacherName, teacherEmail, token string) error {
	link := fmt.Sprintf("%s/verify/teacher?token=%s", s.baseURL, token)
	body := cardLayout(cardArgs{
		LogoURL:     s.logoURL,
		BgURL:       s.bgURL,
		Title:       "教师账号待审批",
		Description: "有一位新教师已注册，需要您审批通过。",
		InfoBlock:   infoBlock(teacherName, teacherEmail),
		ButtonText:  "审批教师账号",
		ButtonLink:  link,
		Footer:      "只需一位管理员审批即可。",
	})
	return s.sendMany(to, "MechHub 教师账号审批", body)
}

func (s *Sender) SendResetEmail(to, token string) error {
	link := fmt.Sprintf("%s/verify/reset-password?token=%s", s.baseURL, token)
	body := cardLayout(cardArgs{
		LogoURL:     s.logoURL,
		BgURL:       s.bgURL,
		Title:       "重置密码",
		Description: "您申请了密码重置。请点击下方按钮设置新密码。",
		ButtonText:  "重置密码",
		ButtonLink:  link,
		Footer:      "如非您本人操作，请忽略此邮件。",
	})
	return s.send(to, "重置您的 MechHub 密码", body)
}

func (s *Sender) send(to, subject, body string) error {
	_, err := s.client.Emails.Send(&resend.SendEmailRequest{
		From:    s.from,
		To:      []string{to},
		Subject: subject,
		Html:    body,
	})
	return err
}

func (s *Sender) sendMany(to []string, subject, body string) error {
	_, err := s.client.Emails.Send(&resend.SendEmailRequest{
		From:    s.from,
		To:      to,
		Subject: subject,
		Html:    body,
	})
	return err
}

type cardArgs struct {
	LogoURL     string
	BgURL       string
	Title       string
	Description string
	InfoBlock   string
	ButtonText  string
	ButtonLink  string
	Footer      string
}

func cardLayout(a cardArgs) string {
	var logo string
	if a.LogoURL != "" {
		logo = fmt.Sprintf(`<img src="%s" width="28" height="28" alt="" style="vertical-align:middle;border:0;display:inline-block;">&emsp;`, a.LogoURL)
	}

	var bgStyle string
	if a.BgURL != "" {
		bgStyle = fmt.Sprintf(`background-image:url(%s);`, a.BgURL)
	}
	bgStyle += `background-color:#0c1929;background-size:cover;`

	var info string
	if a.InfoBlock != "" {
		info = a.InfoBlock
	}

	var b strings.Builder
	b.WriteString(`<!DOCTYPE html>`)
	b.WriteString(`<html>`)
	b.WriteString(`<body style="margin:0;padding:0;background-color:#0c1929;font-family:-apple-system,BlinkMacSystemFont,Segoe UI,Roboto,Helvetica,Arial,sans-serif;">`)

	b.WriteString(`<table width="100%" cellpadding="0" cellspacing="0" role="presentation">`)
	b.WriteString(`<tr>`)
	b.WriteString(`<td align="center" style="` + bgStyle + `padding:40px 16px;"` + bgAttr(a.BgURL) + `>`)

	b.WriteString(`<table width="480" cellpadding="0" cellspacing="0" role="presentation" style="background-color:#ffffff;border-radius:8px;overflow:hidden;border:1px solid #1e3a5f;">`)

	b.WriteString(`<tr>`)
	b.WriteString(`<td style="padding:24px 32px;background-color:#12294a;">`)
	b.WriteString(logo)
	b.WriteString(`<span style="color:#ffffff;font-size:18px;font-weight:700;vertical-align:middle;">MechHub</span>`)
	b.WriteString(`</td>`)
	b.WriteString(`</tr>`)

	b.WriteString(`<tr>`)
	b.WriteString(`<td style="padding:32px 32px 0 32px;font-size:20px;font-weight:600;color:#1e293b;">` + html.EscapeString(a.Title) + `</td>`)
	b.WriteString(`</tr>`)

	b.WriteString(`<tr>`)
	b.WriteString(`<td style="padding:16px 32px 0 32px;font-size:15px;line-height:1.6;color:#475569;">` + html.EscapeString(a.Description) + `</td>`)
	b.WriteString(`</tr>`)

	if info != "" {
		b.WriteString(`<tr>`)
		b.WriteString(`<td style="padding:20px 32px 0 32px;">`)
		b.WriteString(info)
		b.WriteString(`</td>`)
		b.WriteString(`</tr>`)
	}

	b.WriteString(`<tr>`)
	b.WriteString(`<td style="padding:28px 32px 0 32px;">`)
	b.WriteString(`<a href="` + a.ButtonLink + `" style="display:inline-block;padding:12px 32px;background-color:#2563eb;color:#ffffff;font-size:15px;font-weight:600;text-decoration:none;border-radius:6px;">` + html.EscapeString(a.ButtonText) + `</a>`)
	b.WriteString(`</td>`)
	b.WriteString(`</tr>`)

	b.WriteString(`<tr>`)
	b.WriteString(`<td style="padding:12px 32px 32px 32px;font-size:12px;color:#94a3b8;">`)
	b.WriteString(html.EscapeString(a.Footer))
	if a.Footer != "" {
		b.WriteString(`<br>`)
	}
	b.WriteString(`如果按钮无法点击，请复制以下链接：<br>`)
	b.WriteString(`<a href="` + a.ButtonLink + `" style="color:#2563eb;">` + a.ButtonLink + `</a>`)
	b.WriteString(`</td>`)
	b.WriteString(`</tr>`)

	b.WriteString(`</table>`)
	b.WriteString(`</td></tr>`)
	b.WriteString(`</table>`)
	b.WriteString(`</body>`)
	b.WriteString(`</html>`)
	return b.String()
}

func bgAttr(url string) string {
	if url == "" {
		return ""
	}
	return ` background="` + url + `"`
}

func infoBlock(teacherName, teacherEmail string) string {
	var b strings.Builder
	b.WriteString(`<table width="100%" cellpadding="0" cellspacing="0" role="presentation" style="background-color:#eaf2ff;border:1px solid #c5d9ff;border-radius:6px;">`)
	b.WriteString(`<tr><td style="padding:16px 20px;">`)
	b.WriteString(`<table cellpadding="0" cellspacing="0" role="presentation">`)
	b.WriteString(`<tr><td style="font-size:13px;color:#64748b;padding-bottom:4px;">姓名</td></tr>`)
	b.WriteString(`<tr><td style="font-size:15px;color:#1e293b;font-weight:500;">` + html.EscapeString(teacherName) + `</td></tr>`)
	b.WriteString(`<tr><td style="font-size:13px;color:#64748b;padding-top:12px;padding-bottom:4px;">邮箱</td></tr>`)
	b.WriteString(`<tr><td style="font-size:15px;color:#1e293b;font-weight:500;">` + html.EscapeString(teacherEmail) + `</td></tr>`)
	b.WriteString(`<tr><td style="font-size:13px;color:#64748b;padding-top:12px;padding-bottom:4px;">角色</td></tr>`)
	b.WriteString(`<tr><td style="font-size:15px;color:#1e293b;font-weight:500;">教师</td></tr>`)
	b.WriteString(`</table>`)
	b.WriteString(`</td></tr>`)
	b.WriteString(`</table>`)
	return b.String()
}
