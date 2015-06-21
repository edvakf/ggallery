# Ansible config files for setup and deploy ggallery

Install ansible on a local machine and run the following commands at this directory.

You need to be able to login to hosts listed in the `production` file with no password and use sudo on that host.

## setup

```
$ ansible-playbook -i production setup.yml
```

## deploy

```
$ ansible-playbook -i production deploy.yml
```
