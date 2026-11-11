const express = require('express');
const http = require('http');
const https = require('https');
const path = require('path');

const app = express();
const PORT = 8899;

// API 配置（直接连接上游，不走 cc-switch 代理）
const API_CONFIG = {
  baseUrl: process.env.LLM_BASE_URL || 'https://llm-gw-lab.eeo-inc.com',
  apiKey: process.env.LLM_API_KEY || '',
  model: process.env.LLM_MODEL || 'deepseek-v4-pro'
};

// 静态文件
app.use(express.static(__dirname));

// 默认首页
app.get('/', (req, res) => {
  res.redirect('/chat.html');
});

// API 代理（避免 CORS 问题）
app.post('/api/chat', express.json(), (req, res) => {
  const { messages, model } = req.body;

  const payload = {
    model: model || API_CONFIG.model,
    messages: [
      {
        role: 'system',
        content: '你是一个智能助手。你可以使用工具来帮助用户。当需要搜索信息时，调用 search 工具；当需要计算时，调用 calculator 工具；当需要查天气时，调用 weather 工具。如果用户的问题不需要工具，直接回答即可。'
      },
      ...messages
    ]
  };

  const url = new URL(API_CONFIG.baseUrl);
  const requestModule = url.protocol === 'https:' ? https : http;
  const options = {
    hostname: url.hostname,
    port: url.port || (url.protocol === 'https:' ? 443 : 80),
    path: '/v1/chat/completions',
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${API_CONFIG.apiKey}`
    },
    timeout: 60000
  };

  const proxyReq = requestModule.request(options, (proxyRes) => {
    res.status(proxyRes.statusCode);
    proxyRes.pipe(res);
  });

  proxyReq.on('error', (err) => {
    res.status(500).json({ error: 'API 请求失败: ' + err.message });
  });

  proxyReq.write(JSON.stringify(payload));
  proxyReq.end();
});

// 健康检查
app.get('/api/health', (req, res) => {
  res.json({ status: 'ok', model: API_CONFIG.model });
});

app.listen(PORT, () => {
  console.log(`\n  🤖 Agent 对话测试台已启动`);
  console.log(`  → http://localhost:${PORT}\n`);
});
