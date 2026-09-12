import sys
from mail_sender import MailSender

def main():
    if len(sys.argv) < 3:
        print("usage: send_code.py <email> <code>")
        return 1
    to_email = sys.argv[1]
    code = sys.argv[2]
    subject = "旧聊验证码"
    content = f"你的验证码是: {code}\n有效期10分钟。如非本人操作请忽略。"
    sender = MailSender()
    success, msg = sender.send_mail(to_email, subject, content)
    if not success:
        print(msg)
        return 1
    print("ok")
    return 0

if __name__ == "__main__":
    sys.exit(main())
