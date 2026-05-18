# milking — Claude 向けの作業メモ

## 絶対にやらないこと

- **`data.db` を消さない**。このファイルはユーザーが日常的に書き込んでいる本物のデータ。
  検証目的で消したくなっても消さない。スキーマやコードを変更したことの動作確認は、
  既存 DB の中身を保ったまま行う。
  - スキーマを変えるなら `ALTER TABLE` などで段階的に行うか、最低限事前に確認する。
  - 検証用に空 DB が必要なときは `data.db` ではなく別ファイル（例: `/tmp/milking-test.db`）を使う。
- 動作確認後に `milking.log` も削除しない（運用ログとして残す）。
- **本番 `data.db` に対して副作用のあるリクエストを試験的に叩かない**。
  - `POST /feedings` は単に1行追加するだけでなく、同一トランザクションで
    `assignments` を `slot_index -= 1` でシフトし、`next_adjustment` を消す。
    あとで挿入した実績を削除しても、assignments の slot 1 は永久に失われる。
  - 副作用を伴う検証は、別 DB を指して起動するか、Go テスト（`*_test.go`）として書く。
  - 読み取り専用の検証（`GET /` を curl、`sqlite3 data.db "SELECT ..."`）は問題なし。

## ビルド・起動

- 起動: `./start.sh`（`go build` してから `nohup` 起動。PID は `milking.pid`）
- 停止: `./stop.sh`
- `go run main.go` は同 package の `store.go` / `schedule.go` を見つけられないので使わない。
  `go run .` か `./start.sh`。

## 構成メモ

- スキーマは `schema.sql` を `go:embed` して `store.go` で適用。スキーマ変更時はこのファイルを編集。
- 担当者名 (`CaregiverAName` / `CaregiverBName`) は `store.go` のハードコード定数。UI では編集不可。
- スロット計算は「幅一定 (max-min)、min ずつシフト」。`schedule.go::BuildSchedule` 参照。
- 表示スロット数は `schedule.go::scheduleSlotCount`、実績保持件数は `store.go::feedingKeepN`。
