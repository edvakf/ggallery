DROP TABLE IF EXISTS `plot`;
CREATE TABLE `plot` (
  `id` varbinary(5) NOT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `code` text,
  PRIMARY KEY (`id`),
  KEY `created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

DROP TABLE IF EXISTS `file`;
CREATE TABLE `file` (
  `plot_id` varbinary(5) NOT NULL,
  `name` varchar(30) NOT NULL,
  `content` mediumtext,
  KEY `plot_id` (`plot_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
