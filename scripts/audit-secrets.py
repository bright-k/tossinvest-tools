#!/usr/bin/env python3
"""Offline audit of publication candidates and Git objects. Never print secrets.
Requires pinned .tools/gitleaks/gitleaks; bootstrap via scripts/bootstrap-audit.
Index contents are exported read-only; this script never stages or commits files.
"""
import argparse, hashlib, json, pathlib, re, subprocess, tempfile
ROOT=pathlib.Path(__file__).resolve().parent.parent
PINNED_SCHEMA_DIGEST='7e753c21dc4b938d8f9610ce3efd6542783018977cb030e39db5a006f5b59d40'
TOSS_ID=re.compile(r'tsck_(?:live|test)_[A-Za-z0-9_-]+')
PUBLIC_CLIENT_ID='tsck_live_example-client-id'

def git(*args):return subprocess.check_output(['git',*args],cwd=ROOT)
def names(*args):return [x.decode() for x in git(*args).split(b'\0') if x]
def digest(data):return hashlib.sha256(data).hexdigest()
def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--output',type=pathlib.Path);args=parser.parse_args()
    scanner=ROOT/'.tools/gitleaks/gitleaks'
    if not scanner.exists():raise SystemExit('Run ./scripts/bootstrap-audit first.')
    version=subprocess.check_output([str(scanner),'version'],text=True).strip()
    if version!='8.30.1':raise SystemExit('Expected Gitleaks 8.30.1.')
    tracked=names('ls-files','-z');untracked=names('ls-files','--others','--exclude-standard','-z')
    summary={'date':'2026-10-26','timezone':'Asia/Seoul','scanner':'Gitleaks '+version,'network_upload':False,'keychain_read':False,'scopes':{},'supplemental_findings':[],'skipped_symlinks':[]}
    with tempfile.TemporaryDirectory(prefix='ti-secret-audit-',dir='/private/tmp') as td:
        tmp=pathlib.Path(td);index=tmp/'index';tree=tmp/'worktree';objects=tmp/'objects'
        for path in (index,tree,objects):path.mkdir()
        config=tmp/'default.toml';config.write_text('[extend]\nuseDefault = true\n')
        ignore=tmp/'empty-ignore';ignore.write_text('')
        bodies=[];index_oids={}
        known_sums=set((ROOT/'go.sum').read_text().splitlines())
        for row in git('ls-files','--stage','-z').split(b'\0'):
            if not row:continue
            header,name=row.split(b'\t',1);mode,oid,stage=header.split();name=name.decode()
            if stage!=b'0':raise SystemExit('Unmerged index; audit requires resolution.')
            if mode==b'120000':summary['skipped_symlinks'].append({'scope':'index','file':name});continue
            data=git('cat-file','blob',oid.decode());target=index/name;target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(data)
            bodies.append(('index',name,data));index_oids[oid.decode()]=name
            if name=='go.sum':known_sums.update(data.decode().splitlines())
        for name in sorted(set(tracked+untracked)):
            source=ROOT/name
            if source.is_symlink():summary['skipped_symlinks'].append({'scope':'worktree','file':name});continue
            if not source.exists():continue
            data=source.read_bytes();target=tree/name;target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(data);bodies.append(('worktree',name,data))
        # Includes unreachable/staged blobs that a normal git log scan misses.
        blob_count=0
        for row in git('cat-file','--batch-all-objects','--batch-check=%(objectname) %(objecttype)').splitlines():
            oid,kind=row.decode().split()
            if kind!='blob':continue
            data=git('cat-file','blob',oid);(objects/oid).write_bytes(data);bodies.append(('objects',oid,data));blob_count+=1
        for scope,target in [('index',index),('worktree',tree),('objects',objects),('history',ROOT)]:
            report=tmp/(scope+'.json')
            command=[str(scanner),'git' if scope=='history' else 'dir',str(target),'--redact=100','--no-banner','--no-color','--log-level=error','--report-format=json','--report-path='+str(report),'--ignore-gitleaks-allow','--config='+str(config),'--gitleaks-ignore-path='+str(ignore)]
            if scope=='history':command+=['--log-opts=--all']
            result=subprocess.run(command,capture_output=True,text=True)
            if result.returncode not in (0,1):raise SystemExit('Gitleaks failed for '+scope+'; no raw scanner diagnostics printed.')
            findings=json.loads(report.read_text()) if report.exists() else []
            unresolved=[];examples=[];checksums=[]
            for finding in findings:
                raw_path=pathlib.Path(finding['File'])
                candidate=raw_path if raw_path.is_absolute() else target/raw_path
                name=str(raw_path)
                if scope!='history':
                    try:name=str(candidate.relative_to(target))
                    except ValueError:raise SystemExit('Unexpected scanner path.')
                safe={'rule':finding['RuleID'],'file':name,'line':finding['StartLine']}
                # This one public, truncated OAuth example is accepted only if
                # the ENTIRE immutable official file has its original SHA-256.
                is_public=(scope!='history' and candidate.exists() and digest(candidate.read_bytes())==PINNED_SCHEMA_DIGEST and finding['RuleID']=='generic-api-key' and finding['StartLine']==121)
                if scope=='history' and name=='specs/tossinvest/openapi.json' and finding['RuleID']=='generic-api-key' and finding['StartLine']==121:
                    commit=finding.get('Commit','')
                    if re.fullmatch(r'[0-9a-f]{40,64}',commit):
                        original=git('cat-file','blob',commit+':specs/tossinvest/openapi.json')
                        is_public=digest(original)==PINNED_SCHEMA_DIGEST

                is_checksum=False
                if scope=='objects' and candidate.exists():
                    lines=candidate.read_text(errors='replace').splitlines()
                    sum_line=re.compile(r'^[A-Za-z0-9_.~+/-]+ v[^\s]+ h1:[A-Za-z0-9+/]{43}=$')
                    is_checksum=bool(lines) and all(sum_line.fullmatch(line) for line in lines) and lines[finding['StartLine']-1] in known_sums
                (examples if is_public else checksums if is_checksum else unresolved).append(safe)
            count=len(tracked) if scope=='index' else len(set(tracked+untracked)) if scope=='worktree' else blob_count if scope=='objects' else None
            summary['scopes'][scope]={'files_or_blobs':count,'scanner_findings':len(findings),'known_public_examples':examples,'known_dependency_checksums':checksums,'unresolved_findings':unresolved}
        for scope,name,data in bodies:
            text=data.decode('utf-8',errors='ignore')
            for match in TOSS_ID.finditer(text):
                if match.group()!=PUBLIC_CLIENT_ID:summary['supplemental_findings'].append({'scope':scope,'file':name,'rule':'toss-client-id','line':text.count('\n',0,match.start())+1})
            if scope in ('index','worktree') and (pathlib.PurePosixPath(name).name in ('connection.json','owner.sock','server.lock','config.local.json') or pathlib.PurePosixPath(name).name.startswith('.env') and name!='.env.example'):
                summary['supplemental_findings'].append({'scope':scope,'file':name,'rule':'runtime-or-credential-file'})
        summary['history_commits']=int(git('rev-list','--count','--all'))
        summary['indexed_entrypoints']=[name for name in tracked if name.startswith('cmd/')]
        summary['indexed_test_files']=len([name for name in tracked if name.endswith('_test.go')])
        summary['staged_and_worktree_differ_files']=len(names('diff','--name-only','-z'))
        summary['untracked_publish_candidates']=len(untracked)
        summary['pass']=not summary['supplemental_findings'] and not summary['skipped_symlinks'] and all(not value['unresolved_findings'] for value in summary['scopes'].values())
    safe_json=json.dumps(summary,ensure_ascii=False,indent=2)+'\n'
    if args.output:args.output.parent.mkdir(parents=True,exist_ok=True);args.output.write_text(safe_json)
    print(safe_json,end='')
    raise SystemExit(0 if summary['pass'] else 1)
if __name__=='__main__':main()
