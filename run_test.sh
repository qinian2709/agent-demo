#!/bin/bash
# 需要本机已设置 OPENAI_API_KEY / OPENAI_BASE_URL
if [ -z "$OPENAI_API_KEY" ]; then
  echo "请先设置 OPENAI_API_KEY"
  exit 1
fi
if [ -z "$OPENAI_BASE_URL" ]; then
  export OPENAI_BASE_URL="https://llm-gw-lab.eeo-inc.com/v1"
fi
promptfoo eval -c promptfooconfig.yaml --output report.json
echo "Exit code: $?"
