#!/usr/bin/env python3
"""Exercise the actual local TTY account-selection CLI using mock data only."""
import json,os,pathlib,pty,select,signal,socket,subprocess,tempfile,time
ROOT=pathlib.Path(__file__).resolve().parent.parent
BINARY=str(ROOT/'bin/tossinvest-tools')
def prompt(fd,needle):
    data=b'';deadline=time.monotonic()+15
    while time.monotonic()<deadline:
        if not select.select([fd],[],[],1)[0]:continue
        try:part=os.read(fd,65536)
        except OSError:break
        data+=part
        if needle in data:return
    raise RuntimeError('TTY prompt missing: '+needle.decode())
def accounts(app,clear=False):
    master,slave=pty.openpty()
    cmd=[BINARY,'setup','accounts','--backend=mock','--app-dir='+app]
    if clear:cmd+=['--clear']
    proc=subprocess.Popen(cmd,stdin=slave,stdout=slave,stderr=slave);os.close(slave)
    try:
        if not clear:prompt(master,b'Select index:alias');os.write(master,b'1:primary\n')
        prompt(master,b'Type YES:');os.write(master,b'YES\n')
        if proc.wait(timeout=15)!=0:raise RuntimeError('TTY account selection failed')
    finally:
        os.close(master)
        if proc.poll() is None:proc.kill();proc.wait()
def query(app,command,*args,expected=0):
    proc=subprocess.run([BINARY,command,*args,'--json','--backend=mock','--app-dir='+app],capture_output=True,text=True,timeout=30)
    assert proc.returncode==expected,(command,proc.returncode)
    assert not proc.stderr if expected==0 else not proc.stdout
    value=json.loads(proc.stdout if expected==0 else proc.stderr)
    return value

def main():
    with tempfile.TemporaryDirectory(prefix='ti-tty-',dir='/private/tmp') as app:
        with socket.socket() as sock:sock.bind(('127.0.0.1',0));addr='127.0.0.1:'+str(sock.getsockname()[1])
        server=subprocess.Popen([BINARY,'serve','--backend=mock','--milestone=R1','--app-dir='+app,'--listen='+addr],stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True)
        try:
            if not server.stdout.readline():raise RuntimeError('mock startup failed')
            initial=query(app,'accounts')
            assert initial['data']==[] and initial['meta']['account_selection_required'] is True
            assert query(app,'holdings','--account-alias=primary',expected=4)['error']['code']=='PERMISSION_DENIED'
            assert query(app,'prices','--symbols=005930,AAPL')['data']
            assert query(app,'prices',expected=2)['error']['code']=='INVALID_ARGUMENT'
            accounts(app)
            listed=query(app,'accounts')
            assert len(listed['data'])==1 and listed['data'][0]['account_alias']=='primary'
            assert listed['data'][0]['account_no_masked']=='****1111'
            holdings=query(app,'holdings','--account-alias=primary','--symbol=005930')
            assert holdings['data']['items'] and holdings['meta']['account_alias']=='primary'
            serialized=json.dumps([listed,holdings])
            assert 'accountSeq' not in serialized and 'accountNo' not in serialized
            status=json.loads(subprocess.check_output([BINARY,'status','--json','--backend=mock','--app-dir='+app],text=True))
            assert status['account_aliases']==['primary']
            accounts(app,clear=True)
            status=json.loads(subprocess.check_output([BINARY,'status','--json','--backend=mock','--app-dir='+app],text=True))
            assert status['account_aliases']==[]
            assert query(app,'holdings','--account-alias=primary',expected=4)['error']['code']=='PERMISSION_DENIED'
            assert query(app,'accounts')['data']==[]
            # Simulate a crash: stale credential files must not authenticate a stopped server.
            connection=pathlib.Path(app,'mock','connection.json')
            old=json.loads(connection.read_text())
            server.kill();server.wait(timeout=8)
            assert connection.exists()
            stopped=subprocess.run([BINARY,'status','--json','--backend=mock','--app-dir='+app],capture_output=True,text=True)
            assert stopped.returncode==3
            server=subprocess.Popen([BINARY,'serve','--backend=mock','--milestone=R1','--app-dir='+app,'--listen='+addr],stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True)
            if not server.stdout.readline():raise RuntimeError('mock crash restart failed')
            new=json.loads(connection.read_text())
            assert old['credential']!=new['credential'] and old['run_id']!=new['run_id']

        finally:
            server.send_signal(signal.SIGTERM)
            try:server.wait(timeout=8)
            except subprocess.TimeoutExpired:server.kill();server.wait()
        assert not pathlib.Path(app,'mock','connection.json').exists()
        assert query(app,'accounts',expected=3)['error']['code']=='SERVER_UNAVAILABLE'
    print(json.dumps({'client':'built CLI with local PTY','selection':'passed','revocation':'passed','status':'passed','credential_cleanup':'passed','crash_stale_file_rejected':'passed','crash_restart_rotation':'passed','backend':'mock','read_queries':'accounts_holdings_prices_passed','revoked_holdings':'exit_4_permission_denied','no_server_query':'exit_3_no_fallback'}))
if __name__=='__main__':main()
