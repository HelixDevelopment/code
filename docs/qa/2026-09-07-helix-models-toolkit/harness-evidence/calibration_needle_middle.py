import json,ssl,urllib.request
ctx=ssl.create_default_context(); ctx.check_hostname=False; ctx.verify_mode=ssl.CERT_NONE
def call(url,model,content,mt=32):
    body=json.dumps({"model":model,"messages":[{"role":"user","content":content}],"temperature":0,"max_tokens":mt}).encode()
    r=urllib.request.Request(url,data=body,headers={"Content-Type":"application/json"})
    with urllib.request.urlopen(r,timeout=300,context=ctx) as f: return json.loads(f.read())
FILL="alpha bravo charlie delta echo foxtrot golf hotel india juliet "
NEEDLE="MIDKEY-QW3RM8"
def p(nwords,depth):
    total=FILL*(nwords//10+1)
    words=total.split()
    cut=int(len(words)*depth)
    pre=" ".join(words[:cut]); post=" ".join(words[cut:])
    return ("Below is a long log. One line contains a secret key.\n"+pre
            +"\n>>> The secret key is "+NEEDLE+" <<<\n"+post
            +"\n\nWhat is the secret key contained in the log above? Reply with only the key.")
for name,url,model in [("coder","http://127.0.0.1:18434/v1/chat/completions","qwen2.5-coder-3b-instruct-q4_k_m"),
                       ("gateway","https://127.0.0.1:8443/v1/chat/completions","helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190"),
                       ("agent-llm","http://127.0.0.1:7061/v1/chat/completions","helixagent-llm")]:
    for depth in (0.5,):
        try:
            d=call(url,model,p(4000,depth)); c=d["choices"][0]["message"]["content"]
            print(f"{name:10} depth={depth} pt={d.get('usage',{}).get('prompt_tokens')} found={NEEDLE in c} reply={c[:90]!r}")
        except Exception as e: print(f"{name:10} ERR {e}")
