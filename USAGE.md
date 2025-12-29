# Lux 使用手册

> 本文档记录已验证可用的功能与命令行用法。

---

## 目录

- [基本用法](#基本用法)
- [Cookie 认证（高画质）](#cookie-认证高画质)
- [画质选择](#画质选择)
- [输出路径与文件名](#输出路径与文件名)
- [播放列表下载](#播放列表下载)
- [完整参数速查表](#完整参数速查表)
- [典型场景示例](#典型场景示例)

---

## 基本用法

### 查看视频信息（不下载）

```bash
lux -i "https://www.bilibili.com/video/BV1rUK56tEg4"
```

输出示例：

```
 Site:      哔哩哔哩 bilibili.com
 Title:     视频标题
 Type:      video
 Streams:   # All available quality
     [80-7]  -------------------
     Quality:         高清 1080P avc1.640033
     Size:            50.90 MiB
     # download with: lux -f 80-7 ...

     [32-7]  -------------------
     Quality:         清晰 480P avc1.640033
     Size:            24.71 MiB
     # download with: lux -f 32-7 ...
```

### 直接下载（默认最高画质）

```bash
lux "https://www.bilibili.com/video/BV1rUK56tEg4"
```

> 默认按画质优先级从高到低排序，自动选择最高可用画质。

---

## Cookie 认证（高画质）

Bilibili 不登录最高只能下载 480P，1080P 及以上需要提供账号 Cookie。

### 方式一：Cookie 文件（推荐）

1. 浏览器安装插件 **"Get cookies.txt LOCALLY"**（Chrome/Firefox 均有）
2. 登录 bilibili.com 后，用插件导出 `cookies.txt`
3. 使用 `-c` 参数指定文件路径：

```bash
lux -c ~/Downloads/cookies.txt "https://www.bilibili.com/video/BV1rUK56tEg4"
```

### 方式二：直接传入 Cookie 字符串

```bash
lux -c "SESSDATA=xxx; bili_jct=xxx; DedeUserID=xxx" "https://www.bilibili.com/video/BV1rUK56tEg4"
```

### 不同账号类型可用画质上限

| 账号状态 | 最高画质 |
|---------|---------|
| 未登录   | 480P    |
| 普通登录  | 1080P   |
| 大会员   | 1080P+ / 4K / 8K |

---

## 画质选择

### 查看所有可用画质

```bash
lux -i "https://www.bilibili.com/video/BV1rUK56tEg4"
```

### 按画质名称选择（`-q`）

使用 `-q` 参数，支持子串匹配（大小写不敏感）：

```bash
# 选择 1080P
lux -q "1080P" "https://www.bilibili.com/video/BV1rUK56tEg4"

# 选择 720P
lux -q "720P" "https://www.bilibili.com/video/BV1rUK56tEg4"

# 选择 480P
lux -q "480P" "https://www.bilibili.com/video/BV1rUK56tEg4"

# 选择 360P
lux -q "360P" "https://www.bilibili.com/video/BV1rUK56tEg4"
```

> 找不到匹配的画质时，自动回退到最高可用画质。

### 按 Stream ID 精确选择（`-f`）

先用 `-i` 查看 stream ID，再用 `-f` 下载：

```bash
# 查看所有 stream
lux -i "https://www.bilibili.com/video/BV1rUK56tEg4"

# 指定 stream ID 下载（如 80-7 表示 1080P AVC）
lux -f 80-7 "https://www.bilibili.com/video/BV1rUK56tEg4"
```

### 画质 ID 与名称对应表

| Stream ID 前半部分 | 画质名称     |
|-------------------|------------|
| 127               | 超高清 8K   |
| 120               | 超清 4K     |
| 116               | 高清 1080P60|
| 112               | 高清 1080P+ |
| 80                | 高清 1080P  |
| 64                | 高清 720P   |
| 32                | 清晰 480P   |
| 16                | 流畅 360P   |

Stream ID 格式：`{画质ID}-{编码ID}`，编码 ID：`7`=AVC、`12`=HEVC、`13`=AV1

### 优先级规则

```
-f（精确 stream ID）> -q（画质名称）> 默认最高画质
```

---

## 输出路径与文件名

### 指定输出目录（`-o`）

```bash
lux -o ~/Movies "https://www.bilibili.com/video/BV1rUK56tEg4"
```

### 指定输出文件名（`-O`，不含扩展名）

```bash
lux -O "布鲁伊第三集" "https://www.bilibili.com/video/BV1rUK56tEg4"
```

### 同时指定目录和文件名

```bash
lux -o ~/Movies -O "布鲁伊第三集" "https://www.bilibili.com/video/BV1rUK56tEg4"
```

---

## 播放列表下载

使用 `-p` 参数下载整个播放列表（分P视频、合集、番剧列表等）。

### 查看播放列表信息

```bash
lux -p -i "https://www.bilibili.com/video/BV1rUK56tEg4"
```

### 下载整个播放列表

```bash
lux -p "https://www.bilibili.com/video/BV1rUK56tEg4"
```

### 指定下载范围

```bash
# 下载第 2 到第 5 个视频
lux -p --start 2 --end 5 "https://www.bilibili.com/video/BV1rUK56tEg4"

# 指定下载第 1、3、5~7 个视频
lux -p --items 1,3,5-7 "https://www.bilibili.com/video/BV1rUK56tEg4"
```

### 支持的播放列表类型

| 类型 | 说明 | 示例 URL 特征 |
|------|------|--------------|
| 分P视频 | 同一 BV 的多个 Part | URL 带 `?p=` 参数 |
| 视频合集（ugcSeason） | UP 主的合集 | 视频页右侧显示"合集"标签 |
| 多集系列 | 不同 BV 的系列视频 | 视频页右侧显示剧集列表 |
| 番剧 | Bilibili 动漫/番剧 | URL 含 `bangumi` |

---

## 完整参数速查表

| 参数 | 简写 | 说明 |
|------|------|------|
| `--info` | `-i` | 只显示视频信息，不下载 |
| `--cookie` | `-c` | 指定 Cookie 文件路径或 Cookie 字符串 |
| `--quality` | `-q` | 按画质名称选择（子串匹配，如 `1080P`）|
| `--stream-format` | `-f` | 按 Stream ID 精确选择（如 `80-7`）|
| `--output-path` | `-o` | 指定输出目录 |
| `--output-name` | `-O` | 指定输出文件名（不含扩展名）|
| `--playlist` | `-p` | 下载整个播放列表 |
| `--start` | 无 | 播放列表起始序号（默认 1）|
| `--end` | 无 | 播放列表结束序号（默认全部）|
| `--items` | 无 | 指定播放列表项，如 `1,3,5-7` |

---

## 典型场景示例

```bash
# 1. 只查看视频信息
lux -i "https://www.bilibili.com/video/BV1rUK56tEg4"

# 2. 直接下载（默认最高画质）
lux "https://www.bilibili.com/video/BV1rUK56tEg4"

# 3. 带 Cookie 下载 1080P，保存到指定目录
lux -c ~/Downloads/cookies.txt -q "1080P" -o ~/Movies "https://www.bilibili.com/video/BV1rUK56tEg4"

# 4. 下载整个合集/播放列表
lux -c ~/Downloads/cookies.txt -p "https://www.bilibili.com/video/BV1rUK56tEg4"

# 5. 下载播放列表中的第 2~5 个视频，1080P，保存到指定目录
lux -c ~/Downloads/cookies.txt -p -q "1080P" --start 2 --end 5 -o ~/Movies "https://www.bilibili.com/video/BV1rUK56tEg4"

# 6. 用精确 Stream ID 下载（先用 -i 查，再用 -f 下）
lux -i "https://www.bilibili.com/video/BV1rUK56tEg4"
lux -f 80-7 "https://www.bilibili.com/video/BV1rUK56tEg4"
```
