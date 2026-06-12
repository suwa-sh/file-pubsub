# Security Policy

## Supported Versions

最新のリリースのみサポートします。

| Version | Supported |
|---------|-----------|
| 最新リリース (latest release) | ✅ |
| それ以前 | ❌ |

## Reporting a Vulnerability

脆弱性を発見した場合は、**公開 Issue にせず**、GitHub の Private Vulnerability Reporting から報告してください:

https://github.com/suwa-sh/file-pubsub/security/advisories/new

- 報告には再現手順・影響範囲・想定される攻撃シナリオを含めてください
- 受領後はベストエフォートで対応します(個人メンテナンスの OSS です)
- 修正がリリースされるまで、詳細の公開は控えていただけると助かります

## Known Limitations (仕様上の制約)

以下は README の「セキュリティ注記」に記載済みの設計上の制約であり、脆弱性報告の対象外です:

- FTP は平文プロトコルです(信頼できるネットワーク内での利用が前提)
- SFTP / SCP は SSH ホストキー検証を行いません
- ファイル内容は pass-through で、暗号化・マスキングは導入先の責務です
- アクセス制御は OS のファイル権限に依存します
