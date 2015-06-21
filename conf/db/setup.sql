CREATE DATABASE ggallery;
CREATE USER 'ggallery'@'localhost' IDENTIFIED BY 'galeria';
GRANT ALL PRIVILEGES ON ggallery.* TO 'ggallery'@'localhost';
FLUSH PRIVILEGES;
