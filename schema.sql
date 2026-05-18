CREATE TABLE IF NOT EXISTS settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  interval_min_minutes INTEGER NOT NULL,
  interval_max_minutes INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS feedings (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  fed_at_unix INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_feedings_fed_at ON feedings(fed_at_unix DESC);

-- assignments.slot_index は直近 feeding（=アンカー）から見た N 番目（1始まり）の予定枠。
-- 実績記録時、slot_index=1 の行は削除（消費）、slot_index>1 は -1 シフトされる。
-- 未設定 = 行が存在しない、で表現する。
CREATE TABLE IF NOT EXISTS assignments (
  slot_index INTEGER NOT NULL,
  caregiver TEXT NOT NULL CHECK (caregiver IN ('a','b')),
  status TEXT NOT NULL CHECK (status IN ('o','t','x')),
  PRIMARY KEY (slot_index, caregiver)
);

-- 次回ミルクの予定時刻オプション（吐いた／飲まなかった等で通常より早めにしたい時用）。
-- 行があるとスロット1がその時刻に固定され、以降は通常間隔で計算される。
-- 実績記録時に行は削除（消費）される。
CREATE TABLE IF NOT EXISTS next_adjustment (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  target_unix INTEGER NOT NULL,
  reason TEXT NOT NULL DEFAULT ''
);
