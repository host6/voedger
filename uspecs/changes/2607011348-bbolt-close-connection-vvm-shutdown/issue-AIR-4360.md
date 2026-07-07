# bbolt: close the connection on VVM shutdown

- URL: https://untill.atlassian.net/browse/AIR-4360
- ID: AIR-4360
- Type: Sub-task
- State: in-progress
- Author: Unknown
- Assignee: Denis Gribanov (d.gribanov@dev.untill.com)
- Labels: none

## Description

Why

bbolt driver does not close the db connection on VVM shutdown. Test db created in this task could grow up to 128 Mb during test and is not deleted after the test

What

close the bbolt session on IAppStorageProvider.Stop()
