"""Execute fixture queries with GraphQL validation, aliases and field projection."""
import json

from graphql import build_schema, graphql_sync

import run_command_benchmark as core
import run_all_github_benchmark as suite

SDL = '''
scalar GitObjectID
enum IssueState { OPEN CLOSED }
interface Node { id: ID! }
type Query { repository(owner:String!,name:String!):Repository node(id:ID!):Node nodes(ids:[ID!]!):[Node] }
type Mutation { createLinkedBranch(input:CreateLinkedBranchInput!):CreateLinkedBranchPayload }
input CreateLinkedBranchInput { issueId:ID! name:String! oid:GitObjectID! }
type CreateLinkedBranchPayload { linkedBranch:LinkedBranch }
type LinkedBranch { ref:Ref }
type Ref { name:String! repository:Repository }
type Repository {
 nameWithOwner:String!
 issueTemplates:[Template!]! pullRequestTemplates:[Template!]!
 pullRequest(number:Int!):PullRequest
 issues(states:[IssueState!],first:Int,after:String):IssueConnection
}
type Template { filename:String body:String }
type PageInfo { hasNextPage:Boolean! endCursor:String }
type IssueConnection { nodes:[Issue!]! pageInfo:PageInfo! }
type Issue implements Node { id:ID! number:Int! linkedBranches(first:Int,after:String):LinkedBranchConnection! }
type LinkedBranchConnection { nodes:[LinkedBranch!]! pageInfo:PageInfo! }
type Commit { oid:GitObjectID! }
type MergeQueueEntry { id:ID! }
type PullRequest {
 url:String! state:String! isDraft:Boolean! merged:Boolean!
 headRefOid:GitObjectID! baseRefOid:GitObjectID!
 mergeable:String! mergeStateStatus:String! reviewDecision:String
 mergeCommit:Commit potentialMergeCommit:Commit mergeQueueEntry:MergeQueueEntry
 reviewThreads(first:Int,after:String):ReviewThreadConnection!
}
type ReviewThreadConnection { nodes:[PullRequestReviewThread!]! pageInfo:PageInfo! }
type PullRequestReviewThread implements Node {
 id:ID! path:String! line:Int startLine:Int originalLine:Int originalStartLine:Int
 diffSide:String startDiffSide:String isResolved:Boolean! isOutdated:Boolean!
 comments(first:Int,after:String):ReviewCommentConnection!
}
type ReviewCommentConnection { nodes:[PullRequestReviewComment!]! pageInfo:PageInfo! }
type Actor { login:String! }
type PullRequestReviewComment implements Node {
 id:ID! author:Actor body:String! url:String createdAt:String updatedAt:String diffHunk:String
}
'''


def connection(items, first=None, after=None):
    start=int(after.removeprefix('cursor:')) if after else 0
    limit=min(first if first is not None else 100,100)
    if start<0 or limit<1:raise ValueError('invalid cursor or page size')
    nodes=items[start:start+limit];end=start+len(nodes)
    return {'nodes':nodes,'pageInfo':{'hasNextPage':end<len(items),'endCursor':f'cursor:{end}' if nodes else None}}


class FixtureGraphQL:
    def __init__(self, server):
        self.server=server
        self.schema=build_schema(SDL)
        def bind(kind,name,resolver):self.schema.get_type(kind).fields[name].resolve=resolver
        bind('Query','repository',self.repository)
        bind('Query','node',lambda _,info,id:self.node(id))
        bind('Query','nodes',lambda _,info,ids:[self.node(identifier) for identifier in ids])
        bind('Repository','issueTemplates',lambda *_:[])
        bind('Repository','pullRequestTemplates',lambda *_:[])
        bind('Repository','pullRequest',lambda _,info,number:self.pull_request(number))
        bind('Repository','issues',lambda _,info,states=None,first=None,after=None:connection(self.issues(),first,after))
        bind('Issue','linkedBranches',lambda issue,info,first=None,after=None:connection(self.links(issue),first,after))
        bind('PullRequest','reviewThreads',lambda _,info,first=None,after=None:connection(self.threads(),first,after))
        bind('PullRequestReviewThread','comments',lambda thread,info,first=None,after=None:connection(thread['comment_nodes'],first,after))
        bind('Mutation','createLinkedBranch',self.create_branch)

    def repository(self,_,info,owner,name):
        wanted=core.cleanup.REPO if self.server.scenario=='cleanup-apply' else core.REPO
        return {'nameWithOwner':wanted} if owner+'/'+name==wanted else None

    def pull_request(self,number):
        s=self.server
        if number not in (7,42):return None
        return {'url':f'https://github.com/{core.REPO}/pull/{number}','state':'MERGED' if s.merged else 'OPEN',
                'isDraft':False,'merged':s.merged,'headRefOid':s.head,'baseRefOid':s.base,
                'mergeable':'MERGEABLE','mergeStateStatus':'CLEAN','reviewDecision':'APPROVED',
                'mergeCommit':{'oid':suite.MERGE_SHA} if s.merged else None,
                'potentialMergeCommit':None,'mergeQueueEntry':None}

    def threads(self):
        s=self.server
        if s.scenario in ('pr-submit','pr-merge'):return []
        return [{'__typename':'PullRequestReviewThread','id':f'T{i}','path':f'file{i}.go','line':10,
                 'startLine':None,'originalLine':10,'originalStartLine':None,'diffSide':'RIGHT','startDiffSide':None,
                 'isResolved':False,'isOutdated':False,'comment_nodes':[self.comment(i)]} for i in range(1,21)]

    def comment(self,index):
        return {'__typename':'PullRequestReviewComment','id':f'C{index}','author':{'login':'fixture-reviewer'},
                'body':f'Please validate input {index}.','url':f'https://github.com/{core.REPO}/pull/7#discussion_r{index}',
                'createdAt':'2026-10-07T00:00:00Z','updatedAt':'2026-10-07T00:00:00Z','diffHunk':'@@ -1 +1 @@\n-old\n+new'}

    def issues(self):
        s=self.server
        if s.scenario=='cleanup-apply':
            return [dict(item,id='I_'+str(item['number']),__typename='Issue') for item in s.fixture['issues']]
        return []

    def node(self,identifier):
        if identifier.startswith('I_'):
            number=int(identifier[2:])
            return next((item for item in self.issues() if item['number']==number),
                        {'__typename':'Issue','id':identifier,'number':number,'links':self.server.linked})
        if identifier.startswith('C') and identifier[1:].isdigit():return self.comment(int(identifier[1:]))
        return next((thread for thread in self.threads() if thread['id']==identifier),None)

    def links(self,issue):
        s=self.server;repo=core.cleanup.REPO if s.scenario=='cleanup-apply' else core.REPO
        refs=core.cleanup.inventory(s.root/'remote.git','refs/heads/')
        return [{'ref':{'name':name,'repository':{'nameWithOwner':repo}} if 'refs/heads/'+name in refs else None}
                for name in issue['links']]

    def create_branch(self,_,info,input):
        s=self.server
        if input!={'issueId':'I_41','name':core.BRANCH,'oid':s.base_sha} or s.linked:
            raise ValueError('invalid/duplicate linked branch mutation')
        if s.scenario=='issue-create' and not s.created_issues:raise ValueError('issue does not exist')
        core.cleanup.git(s.root/'remote.git','update-ref','refs/heads/'+core.BRANCH,s.base_sha)
        s.linked.append(core.BRANCH)
        return {'linkedBranch':{'ref':{'name':core.BRANCH,'repository':{'nameWithOwner':core.REPO}}}}

    def execute(self,payload):
        result=graphql_sync(self.schema,payload.get('query',''),variable_values=payload.get('variables'),
                            operation_name=payload.get('operationName'))
        return result.formatted
