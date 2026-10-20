use [dbname] 
GO

SET NOCOUNT ON;

PRINT '-- Database roles';
SELECT
    'CREATE ROLE ' + QUOTENAME(name) + ';' AS Script
FROM sys.database_principals
WHERE type = 'R'
  AND is_fixed_role = 0
  AND name NOT IN ('public')
ORDER BY name;


PRINT '-- Database users';
SELECT authentication_type_desc,
    CASE
        WHEN authentication_type_desc = 'INSTANCE'
            THEN 'CREATE USER ' + QUOTENAME(name) + ' FOR LOGIN ' + QUOTENAME(SUSER_SNAME(sid)) + ';'
        WHEN authentication_type_desc = 'DATABASE'
            THEN 'CREATE USER ' + QUOTENAME(name) + ' WITH PASSWORD = ''ChangeMe123!'';'
        WHEN authentication_type_desc = 'WINDOWS'
            THEN 'CREATE USER ' + QUOTENAME(name) + ' FOR LOGIN ' + QUOTENAME(name) + ';'
                        WHEN authentication_type_desc = 'EXTERNAL'
            THEN 'CREATE USER ' + QUOTENAME(name) + ' FOR LOGIN ' + QUOTENAME(name) + ';'
        ELSE
            'CREATE USER ' + QUOTENAME(name) + ' WITHOUT LOGIN;'
    END AS Script
FROM sys.database_principals
WHERE type IN ('S', 'U', 'G', 'E', 'X')
  AND name NOT IN ('dbo', 'guest', 'INFORMATION_SCHEMA', 'sys')
ORDER BY name;


PRINT '-- Role memberships';
SELECT
    'ALTER ROLE ' + QUOTENAME(r.name) +
    ' ADD MEMBER ' + QUOTENAME(m.name) + ';' AS Script
FROM sys.database_role_members drm
JOIN sys.database_principals r
    ON drm.role_principal_id = r.principal_id
JOIN sys.database_principals m
    ON drm.member_principal_id = m.principal_id
WHERE r.name not in ( 'public')
            and m.name !='dbo'
ORDER BY r.name, m.name;


PRINT '-- Explicit permissions / grants';
SELECT
    CASE dp.state
        WHEN 'G' THEN 'GRANT '
        WHEN 'W' THEN 'GRANT '
        WHEN 'D' THEN 'DENY '
        WHEN 'R' THEN 'REVOKE '
    END
    + dp.permission_name
    + CASE
        WHEN dp.class = 0 THEN ''
        WHEN dp.class = 1 THEN
            ' ON ' + QUOTENAME(OBJECT_SCHEMA_NAME(dp.major_id)) + '.' + QUOTENAME(OBJECT_NAME(dp.major_id))
        WHEN dp.class = 3 THEN
            ' ON SCHEMA::' + QUOTENAME(SCHEMA_NAME(dp.major_id))
        ELSE
            ' -- Unsupported permission class: ' + dp.class_desc
      END
    + ' TO ' + QUOTENAME(USER_NAME(dp.grantee_principal_id))
    + CASE
        WHEN dp.state = 'W' THEN ' WITH GRANT OPTION'
        ELSE ''
      END
    + ';' AS Script
FROM sys.database_permissions dp
WHERE dp.grantee_principal_id NOT IN (
    DATABASE_PRINCIPAL_ID('public'),
    DATABASE_PRINCIPAL_ID('dbo'),
    DATABASE_PRINCIPAL_ID('guest'),
    DATABASE_PRINCIPAL_ID('INFORMATION_SCHEMA'),
    DATABASE_PRINCIPAL_ID('sys')
)
ORDER BY USER_NAME(dp.grantee_principal_id), dp.class, dp.permission_name;

---------------------------
--- Orphaned Users 

-- ALTER USER [database_user] WITH LOGIN = [server_login];
other than the intended recipient is prohibited. If you have received this in error, please notify the sender and delete the material from your system.
