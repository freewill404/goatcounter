begin;
	alter table hits       add column code integer not null default 200 check(code >= 100 and code <= 599);
	alter table hits_stats add column code integer not null default 200 check(code >= 100 and code <= 599);
	insert into version values ('2019-08-28-1-code');
commit;
