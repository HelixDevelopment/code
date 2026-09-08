import json,ssl,urllib.request,sys
ctx=ssl.create_default_context(); ctx.check_hostname=False; ctx.verify_mode=ssl.CERT_NONE
def call(url,model,content,mt=16):
    body=json.dumps({"model":model,"messages":[{"role":"user","content":content}],"temperature":0,"max_tokens":mt}).encode()
    r=urllib.request.Request(url,data=body,headers={"Content-Type":"application/json"})
    with urllib.request.urlopen(r,timeout=300,context=ctx) as f:
        return json.loads(f.read())
FILL=("alpha bravo charlie delta echo foxtrot golf hotel india juliet ")
def prompt(nwords):
    reps=nwords//10+1
    return "Reply with the single word OK.\n"+(FILL*reps)
targets=[("coder","http://127.0.0.1:18434/v1/chat/completions","qwen2.5-coder-3b-instruct-q4_k_m"),
         ("gateway","https://127.0.0.1:8443/v1/chat/completions","helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190"),
         ("agent-llm","http://127.0.0.1:7061/v1/chat/completions","helixagent-llm")]
for name,url,model in targets:
    row=[]
    for n in (500,4000):
        try:
            d=call(url,model,prompt(n))
            row.append(d.get("usage",{}).get("prompt_tokens"))
        except Exception as e:
            row.append("ERR:%s"%e); break
    try: ratio=round(row[1]/row[0],2)
    except Exception: ratio="n/a"
    print(f"{name:10} 500w={row[0]} 4000w={row[1] if len(row)>1 else '-'} ratio={ratio}")
