#!/usr/bin/env python3
"""Use installed Codex's actual MCP client for discovery, without creating a chat/LLM turn.
Never print helper headers or raw client logs. Uses mock only and temporary app roots.
"""
import json, os, pathlib, selectors, shutil, signal, socket, subprocess, tempfile, time, shlex, queue, threading
ROOT=pathlib.Path(__file__).resolve().parent.parent
BINARY=ROOT/'bin/tossinvest-tools'
CODEX=shutil.which('codex')

def rpc(proc, method, params, request_id):
    proc.stdin.write(json.dumps({'id':request_id,'method':method,'params':params})+'\n');proc.stdin.flush()
    deadline=time.monotonic()+40
    sel=selectors.DefaultSelector();sel.register(proc.stdout,selectors.EVENT_READ)
    while time.monotonic()<deadline:
        if not sel.select(timeout=1):
            if proc.poll() is not None:raise RuntimeError('Codex app-server exited; inspect private test diagnostics')
            continue
        line=proc.stdout.readline()
        if not line:raise RuntimeError('Codex app-server closed output before response')
        try:msg=json.loads(line)
        except ValueError:continue
        if msg.get('id')==request_id:
            if 'error' in msg:raise RuntimeError('Codex RPC error: '+json.dumps(msg['error']))
            return msg['result']
    raise RuntimeError('Codex RPC timeout: '+method)

def probe(milestone):
    with tempfile.TemporaryDirectory(prefix='ti-cx-',dir='/private/tmp') as app:
        with socket.socket() as sock:sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
        addr=f'127.0.0.1:{port}'
        server=subprocess.Popen([str(BINARY),'serve','--backend=mock','--milestone='+milestone,'--app-dir='+app,'--listen='+addr],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
        codex=None
        try:
            sel=selectors.DefaultSelector();sel.register(server.stdout,selectors.EVENT_READ)
            if not sel.select(timeout=15):raise RuntimeError('mock server startup timeout')
            if not server.stdout.readline():raise RuntimeError('mock server startup failed')
            # Process-local config only. Does not edit user Codex registration or create a chat.
            helper=json.dumps(shlex.join([str(BINARY),'mcp-auth-header','--backend=mock','--app-dir='+app]))
            command=[CODEX,'app-server','--stdio','-c','analytics.enabled=false','-c','mcp_servers.tossinvest_test.url='+json.dumps('http://'+addr+'/mcp'),'-c','mcp_servers.tossinvest_test.http_headers_helper='+helper]
            with tempfile.TemporaryFile(mode='w+t') as private_log:
                codex=subprocess.Popen(command,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=private_log,text=True,bufsize=1)
                try:
                    init=rpc(codex,'initialize',{'clientInfo':{'name':'tossinvest-contract-check','title':'TossInvest mock contract check','version':'0.2.0'}},1)
                    codex.stdin.write(json.dumps({'method':'initialized','params':{}})+'\n');codex.stdin.flush()
                    inventory=rpc(codex,'mcpServerStatus/list',{'serverName':'tossinvest_test','detail':'toolsAndAuthOnly'},2)
                except Exception:
                    private_log.seek(0);lines=private_log.read()
                    # Configuration parse errors contain only non-secret process arguments.
                    safe=[line for line in lines.splitlines() if ('invalid type' in line or 'missing field' in line or 'http_headers_helper' in line)]
                    if safe:print(json.dumps({'configuration_errors':safe[-8:]},ensure_ascii=False))
                    raise
            entries=inventory.get('data',[])
            match=next((v for v in entries if v.get('name')=='tossinvest_test'),None)
            if match is None:raise RuntimeError('configured server absent in Codex inventory')
            tools=match.get('tools',{})
            names=sorted(tools.keys()) if isinstance(tools,dict) else sorted(v['name'] for v in tools)
            want=35 if milestone=='R1' else 39
            if len(names)!=want:raise RuntimeError(f'Codex discovered {len(names)} tools; wanted {want}; safe fields='+json.dumps({k:v for k,v in match.items() if k not in ['tools','resources','resourceTemplates']}))
            return {'milestone':milestone,'client':'Codex app-server','discovered_tools':len(names),'tool_names':names,'discovery':'passed','agent_turn_calls':'not_run_no_user_chat_created','persistent_desktop_registration':'not_changed'}
        finally:
            if codex is not None:
                codex.terminate()
                try:codex.wait(timeout=8)
                except subprocess.TimeoutExpired:codex.kill();codex.wait()
            server.send_signal(signal.SIGTERM)
            try:server.wait(timeout=8)
            except subprocess.TimeoutExpired:server.kill();server.wait()

if __name__=='__main__':
    if not CODEX:raise SystemExit('codex not found; protocol check remains independently available')
    version=subprocess.run([CODEX,'--version'],capture_output=True,text=True,check=True).stdout.strip()
    result={'client_version':version,'transport':'authenticated Streamable HTTP','authentication':'http_headers_helper subprocess; headers captured only by client','results':[probe(m) for m in ['R1','R2']]}
    print(json.dumps(result,ensure_ascii=False,indent=2))
