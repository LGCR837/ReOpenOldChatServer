import os
import json
import urllib.request
import urllib.parse
import urllib.error


class MailSender:
    """通过 SendCloud API 发送邮件"""

    API_URL = "https://api2.sendcloud.net/api/mail/send"

    def __init__(self, api_user=None, api_key=None):
        self.api_user = api_user or os.getenv("SENDLOUD_API_USER") or os.getenv("SENDCLOUD_API_USER", "crmomentsystememail")
        self.api_key = api_key or os.getenv("SENDLOUD_API_KEY") or os.getenv("SENDCLOUD_API_KEY", "5f3c1bae18152f9a49c45a607e61c10e")
        self.from_email = os.getenv("SENDCLOUD_FROM_EMAIL") or (self.api_user + "@mail.crweb.ccwu.cc")
        self.from_name = os.getenv("SENDCLOUD_FROM_NAME", "CRMoment")

    def send_mail(self, to_email, subject, content):
        """
        发送邮件

        :param to_email: 收件人邮箱（多个用英文逗号分隔）
        :param subject: 邮件主题
        :param content: 邮件正文（纯文本）
        :return: (bool, str)
        """
        if not self.api_user or not self.api_key:
            return False, "mail credentials not configured; set SENDCLOUD_API_USER and SENDCLOUD_API_KEY"

        post_fields = {
            "apiUser": self.api_user,
            "apiKey": self.api_key,
            "from": self.from_email,
            "fromName": self.from_name,
            "to": to_email,
            "subject": subject,
            "plain": content,
        }

        data = urllib.parse.urlencode(post_fields).encode("utf-8")

        req = urllib.request.Request(self.API_URL, data=data, method="POST")
        req.add_header("Content-Type", "application/x-www-form-urlencoded")

        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                body = resp.read().decode("utf-8")
        except urllib.error.HTTPError as e:
            err_body = e.read().decode("utf-8", errors="replace")
            return False, "HTTP {}: {}".format(e.code, err_body)
        except urllib.error.URLError as e:
            return False, "网络错误: {}".format(e.reason)
        except Exception as e:
            return False, str(e)

        try:
            result = json.loads(body)
        except (json.JSONDecodeError, ValueError):
            return False, "API 响应格式异常: {}".format(body)

        if result.get("result") is True:
            return True, "发送成功"

        error_msg = result.get("message", "未知错误")
        return False, "发送失败: {}".format(error_msg)


if __name__ == "__main__":
    sender = MailSender()
    print("MailSender (SendCloud) 已就绪。")
    print("API User:", sender.api_user)
    print("From:", sender.from_email)
