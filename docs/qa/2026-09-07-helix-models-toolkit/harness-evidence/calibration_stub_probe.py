import json,urllib.request
def call(model,content,mt=64):
    body=json.dumps({"model":model,"messages":[{"role":"user","content":content}],"temperature":0,"max_tokens":mt}).encode()
    r=urllib.request.Request("http://127.0.0.1:7061/v1/chat/completions",data=body,headers={"Content-Type":"application/json"})
    with urllib.request.urlopen(r,timeout=600) as f: return json.loads(f.read())
for m in ["helixagent-debate","helixagent-ensemble","helix-debate","helix-llm","helixagent-llm"]:
    try:
        a=call(m,"What is the capital of France? One word.")
        b=call(m,"Write a Python function that reverses a string.")
        ca=a["choices"][0]["message"]["content"]; cb=b["choices"][0]["message"]["content"]
        print(f"--- {m}: usage_a={a.get('usage')} identical={ca==cb}")
        print(f"    A={ca[:110]!r}")
        print(f"    B={cb[:110]!r}")
    except Exception as e: print(f"--- {m} ERR {e}")
