# 在线演示的数据

在线演示（`VITE_DEMO=true` 构建，随文档发布在 `/flyx/demo/`）没有后端：`src/demo/index.ts` 在浏览器里用 `src/demo/fixtures.json` 回答所有接口。这些回答是从一个装好示例数据的真实面板上录下来的，页面改了接口以后按下面的步骤重新录一遍。

1. 用空数据库启动后端：`DB_PATH=/tmp/demo/gost.db SERVER_ADDR=127.0.0.1:16370 go run ./cmd/paneld`
2. 依次运行 `seed_1_inbounds.py`、`seed_2_relays_users.py`、`seed_3_subscriptions_history.py`，参数是数据库文件路径
3. 前端开发服务器指向这个后端：`VITE_API_BASE=http://127.0.0.1:16370 pnpm exec vite --port 3114`
4. 录制：`CHROMIUM=/path/to/chrome node record_pages.mjs raw.json`，再 `node record_items.mjs raw.json`（需要 `playwright-core`）
5. 生成：`python3 build_fixtures.py raw.json ../../src/demo/fixtures.json`。没有节点 agent 时录不到的延迟、回程、内核状态由它补上

演示里的时间会按“录制到现在”的间隔整体后移，所以不用经常重录。

## CI 检查

CI 会打包演示站，用无头 Chrome 把每个页面（管理员和普通用户）走一遍：页面报错、被弹回登录页、或者请求了没录到的接口（控制台里的 `[demo] not recorded: …`）都会让 CI 失败。本地跑：

```bash
VITE_DEMO=true VITE_BASE=/flyx/demo/ pnpm exec vite build --outDir dist-demo
node scripts/demo/check.mjs dist-demo   # CHROME=/path/to/chrome 指定浏览器
```

新功能加了接口，失败信息会列出缺的路径：按上面的步骤重录，或者把新接口加进 `record_items.mjs`。新页面要加进 `check.mjs` 的页面列表。
